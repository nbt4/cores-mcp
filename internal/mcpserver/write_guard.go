package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const (
	writeReplayTTL = 24 * time.Hour
	writeReplayMax = 2048
)

var validIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

// MutationControl is embedded into every guided write input. Dry-run always
// suppresses confirmation before the business handler is called. Confirmed
// executions require an idempotency key.
type MutationControl struct {
	DryRun         bool   `json:"dry_run,omitempty" jsonschema:"Validate and return the exact preview without changing data, even if a confirmation field is true."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Required for confirmed execution; 8-128 safe characters. Reusing it with the same payload replays the first result."`
}

type writeReplay struct {
	fingerprint string
	createdAt   time.Time
	done        chan struct{}
	data        any
	sources     []Source
	warnings    []string
	err         error
}

type writeReplayStore struct {
	mu      sync.Mutex
	entries map[string]*writeReplay
}

var mutationReplays = writeReplayStore{entries: make(map[string]*writeReplay)}

type preparedWriteInvocation[In any] struct {
	Input       In
	DryRun      bool
	Confirmed   bool
	ReplayKey   string
	Fingerprint string
}

type mutationContextKey struct{}
type mutationPermissionContextKey struct{}

func withMutationIdempotency(ctx context.Context, key string) context.Context {
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, mutationContextKey{}, key)
}

func mutationIdempotencyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(mutationContextKey{}).(string)
	return value
}

func withMutationPermission(ctx context.Context, permission string) context.Context {
	return context.WithValue(ctx, mutationPermissionContextKey{}, permission)
}

func mutationPermissionFromContext(ctx context.Context) string {
	value, _ := ctx.Value(mutationPermissionContextKey{}).(string)
	return value
}

func prepareWriteInvocation[In any](ctx context.Context, tool string, input In) (preparedWriteInvocation[In], error) {
	copyValue := input
	dryRun, idempotencyKey := mutationControls(input)
	confirmed := mutationConfirmed(input)
	if dryRun {
		copyValue = clearMutationConfirmations(input)
		confirmed = false
	}
	prepared := preparedWriteInvocation[In]{Input: copyValue, DryRun: dryRun, Confirmed: confirmed}
	if !confirmed {
		return prepared, nil
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !validIdempotencyKey.MatchString(idempotencyKey) {
		return prepared, errorsNew("confirmed writes require idempotency_key with 8-128 letters, digits, '.', '_', ':', or '-'")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return prepared, fmt.Errorf("encode idempotent write: %w", err)
	}
	digest := sha256.Sum256(encoded)
	subject := "unknown"
	if info := auth.TokenInfoFromContext(ctx); info != nil && info.UserID != "" {
		subject = info.UserID
	}
	prepared.ReplayKey = subject + "\x00" + tool + "\x00" + idempotencyKey
	prepared.Fingerprint = hex.EncodeToString(digest[:])
	return prepared, nil
}

func mutationControls(input any) (bool, string) {
	value := dereferenceValue(reflect.ValueOf(input))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false, ""
	}
	dryRun := value.FieldByName("DryRun")
	key := value.FieldByName("IdempotencyKey")
	return dryRun.IsValid() && dryRun.Kind() == reflect.Bool && dryRun.Bool(), valueString(key)
}

func mutationConfirmed(input any) bool {
	value := dereferenceValue(reflect.ValueOf(input))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}
	for index := 0; index < value.NumField(); index++ {
		field := value.Type().Field(index)
		candidate := value.Field(index)
		if strings.HasPrefix(field.Name, "Confirm") && candidate.Kind() == reflect.Bool && candidate.Bool() {
			return true
		}
	}
	return false
}

func clearMutationConfirmations[In any](input In) In {
	copyValue := reflect.ValueOf(input)
	if copyValue.Kind() == reflect.Pointer {
		if copyValue.IsNil() {
			return input
		}
		clone := reflect.New(copyValue.Elem().Type())
		clone.Elem().Set(copyValue.Elem())
		clearConfirmationFields(clone.Elem())
		return clone.Interface().(In)
	}
	clone := reflect.New(copyValue.Type()).Elem()
	clone.Set(copyValue)
	clearConfirmationFields(clone)
	return clone.Interface().(In)
}

func clearConfirmationFields(value reflect.Value) {
	value = dereferenceValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	for index := 0; index < value.NumField(); index++ {
		field := value.Type().Field(index)
		candidate := value.Field(index)
		if strings.HasPrefix(field.Name, "Confirm") && candidate.CanSet() && candidate.Kind() == reflect.Bool {
			candidate.SetBool(false)
		}
	}
}

func dereferenceValue(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func valueString(value reflect.Value) string {
	if value.IsValid() && value.Kind() == reflect.String {
		return value.String()
	}
	return ""
}

func (s *writeReplayStore) begin(ctx context.Context, key, fingerprint string) (*writeReplay, bool, error) {
	if key == "" {
		return nil, true, nil
	}
	for {
		s.mu.Lock()
		s.pruneLocked(time.Now())
		if existing := s.entries[key]; existing != nil {
			if existing.fingerprint != fingerprint {
				s.mu.Unlock()
				return nil, false, errorsNew("idempotency_key was already used with a different payload")
			}
			done := existing.done
			s.mu.Unlock()
			select {
			case <-done:
				return existing, false, nil
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
		entry := &writeReplay{fingerprint: fingerprint, createdAt: time.Now(), done: make(chan struct{})}
		s.entries[key] = entry
		s.mu.Unlock()
		return entry, true, nil
	}
}

func (s *writeReplayStore) finish(key string, entry *writeReplay, data any, sources []Source, warnings []string, err error) {
	if key == "" || entry == nil {
		return
	}
	s.mu.Lock()
	entry.data, entry.sources, entry.warnings, entry.err = data, append([]Source(nil), sources...), append([]string(nil), warnings...), err
	close(entry.done)
	s.mu.Unlock()
}

func (s *writeReplayStore) pruneLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.Sub(entry.createdAt) > writeReplayTTL {
			select {
			case <-entry.done:
				delete(s.entries, key)
			default:
			}
		}
	}
	if len(s.entries) < writeReplayMax {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range s.entries {
		select {
		case <-entry.done:
			if oldestKey == "" || entry.createdAt.Before(oldestTime) {
				oldestKey, oldestTime = key, entry.createdAt
			}
		default:
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

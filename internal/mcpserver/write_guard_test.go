package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	coresauth "github.com/nbt4/cores-mcp/internal/authn"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestPrepareWriteInvocationDryRunSuppressesConfirmation(t *testing.T) {
	input := JobUpdateInput{
		MutationControl: MutationControl{DryRun: true},
		JobID:           42,
		Description:     "Updated",
		ConfirmUpdate:   true,
	}
	prepared, err := prepareWriteInvocation(context.Background(), "rental.jobs.update", input)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.DryRun || prepared.Confirmed || prepared.Input.ConfirmUpdate {
		t.Fatalf("dry-run did not suppress confirmation: %#v", prepared)
	}
	if !input.ConfirmUpdate {
		t.Fatal("preparation mutated the caller's input")
	}
}

func TestPrepareWriteInvocationRequiresSafeIdempotencyKey(t *testing.T) {
	input := JobUpdateInput{JobID: 42, ConfirmUpdate: true}
	if _, err := prepareWriteInvocation(context.Background(), "rental.jobs.update", input); err == nil || !strings.Contains(err.Error(), "idempotency_key") {
		t.Fatalf("missing idempotency key error = %v", err)
	}
	input.IdempotencyKey = "short"
	if _, err := prepareWriteInvocation(context.Background(), "rental.jobs.update", input); err == nil || !strings.Contains(err.Error(), "idempotency_key") {
		t.Fatalf("unsafe idempotency key error = %v", err)
	}
	input.IdempotencyKey = "jobs-update-42-v2"
	prepared, err := prepareWriteInvocation(context.Background(), "rental.jobs.update", input)
	if err != nil || !prepared.Confirmed || prepared.ReplayKey == "" || prepared.Fingerprint == "" {
		t.Fatalf("valid preparation = %#v, err=%v", prepared, err)
	}
}

func TestWriteReplayStoreDeduplicatesAndRejectsChangedPayload(t *testing.T) {
	store := writeReplayStore{entries: make(map[string]*writeReplay)}
	entry, owner, err := store.begin(context.Background(), "user\x00tool\x00request-1", "payload-a")
	if err != nil || !owner || entry == nil {
		t.Fatalf("first begin = entry=%#v owner=%v err=%v", entry, owner, err)
	}
	store.finish("user\x00tool\x00request-1", entry, map[string]any{"id": 7}, []Source{{Service: "RentalCore", Entity: "job", ID: "7"}}, []string{"first"}, nil)

	replayed, owner, err := store.begin(context.Background(), "user\x00tool\x00request-1", "payload-a")
	if err != nil || owner || replayed != entry {
		t.Fatalf("replay = entry=%#v owner=%v err=%v", replayed, owner, err)
	}
	if _, owner, err = store.begin(context.Background(), "user\x00tool\x00request-1", "payload-b"); err == nil || owner || !strings.Contains(err.Error(), "different payload") {
		t.Fatalf("changed payload = owner=%v err=%v", owner, err)
	}
}

func TestWriteReplayStoreRetainsFailedAttempt(t *testing.T) {
	store := writeReplayStore{entries: make(map[string]*writeReplay)}
	entry, owner, err := store.begin(context.Background(), "user\x00tool\x00request-error", "payload")
	if err != nil || !owner {
		t.Fatalf("first begin owner=%v err=%v", owner, err)
	}
	wantErr := errors.New("upstream response was ambiguous")
	store.finish("user\x00tool\x00request-error", entry, nil, nil, []string{"check target state"}, wantErr)
	replayed, owner, err := store.begin(context.Background(), "user\x00tool\x00request-error", "payload")
	if err != nil || owner || !errors.Is(replayed.err, wantErr) {
		t.Fatalf("failed replay entry=%#v owner=%v err=%v", replayed, owner, err)
	}
}

func TestExecuteMutationToolRunsConfirmedWriteOnce(t *testing.T) {
	mutationReplays = writeReplayStore{entries: make(map[string]*writeReplay)}
	ctx := writeScopeContext(t)
	input := RequirementUpdateInput{
		MutationControl: MutationControl{IdempotencyKey: "requirement-7-v2"},
		RequirementID:   7,
		Quantity:        3,
		ConfirmUpdate:   true,
	}
	var calls atomic.Int32
	fn := func(ctx context.Context, got RequirementUpdateInput) (any, []Source, []string, error) {
		calls.Add(1)
		if mutationIdempotencyFromContext(ctx) != input.IdempotencyKey {
			t.Fatalf("idempotency context = %q", mutationIdempotencyFromContext(ctx))
		}
		return map[string]any{"id": 7}, []Source{{Service: "RentalCore", Entity: "requirement", ID: "7"}}, nil, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, output, err := executeMutationTool(ctx, "rental.requirements.update", "write", input, fn)
		if err != nil || result != nil || output.Data == nil {
			t.Fatalf("attempt %d result=%#v output=%#v err=%v", attempt, result, output, err)
		}
		if attempt == 1 && !containsString(output.Warnings, "Idempotent replay: no additional mutation was executed.") {
			t.Fatalf("replay warning = %#v", output.Warnings)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

func TestExecuteMutationToolCoalescesConcurrentRetries(t *testing.T) {
	mutationReplays = writeReplayStore{entries: make(map[string]*writeReplay)}
	ctx := writeScopeContext(t)
	input := JobUpdateInput{
		MutationControl: MutationControl{IdempotencyKey: "concurrent-job-update-9"},
		JobID:           9,
		ConfirmUpdate:   true,
	}
	var calls atomic.Int32
	fn := func(context.Context, JobUpdateInput) (any, []Source, []string, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return map[string]any{"id": 9}, nil, nil, nil
	}
	const attempts = 8
	var wait sync.WaitGroup
	errors := make(chan error, attempts)
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func() {
			defer wait.Done()
			result, _, err := executeMutationTool(ctx, "rental.jobs.update", "write", input, fn)
			if err != nil {
				errors <- err
				return
			}
			if result != nil && result.IsError {
				errors <- errorsNew("mutation returned a tool error")
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

func TestExecuteMutationToolDryRunNeverExecutesConfirmedBranch(t *testing.T) {
	ctx := writeScopeContext(t)
	input := JobUpdateInput{MutationControl: MutationControl{DryRun: true}, JobID: 9, ConfirmUpdate: true}
	result, output, err := executeMutationTool(ctx, "rental.jobs.update", "write", input, func(_ context.Context, got JobUpdateInput) (any, []Source, []string, error) {
		if got.ConfirmUpdate {
			t.Fatal("dry-run reached handler with confirmation enabled")
		}
		return map[string]any{"confirmation_required": true}, nil, nil, nil
	})
	if err != nil || result != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	data, ok := output.Data.(map[string]any)
	if !ok || data["dry_run"] != true || !containsString(output.Warnings, "Dry-run only: no data was changed.") {
		t.Fatalf("dry-run output = %#v", output)
	}
}

func TestAuthorizeMutationEnforcesServiceAndActionScope(t *testing.T) {
	rentalCreate := writeScopeContextWithScopes(t, coresauth.ReadScope(), coresauth.ServiceWriteScope("rental", "create"))
	if _, err := authorizeMutation(rentalCreate, "rental.jobs.create"); err != nil {
		t.Fatalf("rental create was rejected: %v", err)
	}
	if _, err := authorizeMutation(rentalCreate, "rental.jobs.update"); err == nil {
		t.Fatal("rental create scope authorized an update")
	}
	if _, err := authorizeMutation(rentalCreate, "warehouse.products.create"); err == nil {
		t.Fatal("rental create scope authorized a warehouse create")
	}
	procurementApprove := writeScopeContextWithScopes(t, coresauth.ReadScope(), coresauth.ServiceWriteScope("procurement", "approve"))
	if _, err := authorizeMutation(procurementApprove, "procurement.requisitions.decide"); err != nil {
		t.Fatalf("procurement approval was rejected: %v", err)
	}
	if _, err := authorizeMutation(procurementApprove, "procurement.orders.transition"); err != nil {
		t.Fatalf("procurement order approval was rejected: %v", err)
	}
	if _, err := authorizeMutation(procurementApprove, "procurement.orders.receive"); err == nil {
		t.Fatal("procurement approval scope authorized goods receipt")
	}
	if _, err := authorizeMutation(procurementApprove, "procurement.requisitions.submit"); err == nil {
		t.Fatal("procurement approval scope authorized requisition submission")
	}
	procurementSubmit := writeScopeContextWithScopes(t, coresauth.ReadScope(), coresauth.ServiceWriteScope("procurement", "submit"))
	if _, err := authorizeMutation(procurementSubmit, "procurement.requisitions.submit"); err != nil {
		t.Fatalf("procurement submission was rejected: %v", err)
	}
	if _, err := authorizeMutation(procurementSubmit, "procurement.requisitions.decide"); err == nil {
		t.Fatal("procurement submit scope authorized a decision")
	}
	if _, err := authorizeMutation(procurementSubmit, "procurement.orders.transition"); err == nil {
		t.Fatal("procurement submit scope authorized an order transition")
	}
	procurementCreate := writeScopeContextWithScopes(t, coresauth.ReadScope(), coresauth.ServiceWriteScope("procurement", "create"))
	if _, err := authorizeMutation(procurementCreate, "procurement.suppliers.update"); err == nil {
		t.Fatal("procurement create scope authorized supplier update")
	}
	procurementUpdate := writeScopeContextWithScopes(t, coresauth.ReadScope(), coresauth.ServiceWriteScope("procurement", "update"))
	if _, err := authorizeMutation(procurementUpdate, "procurement.suppliers.update"); err != nil {
		t.Fatalf("procurement supplier update was rejected: %v", err)
	}
	if _, err := authorizeMutation(procurementUpdate, "procurement.suppliers.create"); err == nil {
		t.Fatal("procurement update scope authorized supplier creation")
	}
}

func TestCoreAPIForwardsIdempotencyKey(t *testing.T) {
	secret := strings.Repeat("i", 48)
	var header string
	var origin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("Idempotency-Key")
		origin = r.Header.Get("X-Cores-Origin")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	api := newCoreAPIClient(testConfigWithSecret(secret))
	ctx := withMutationIdempotency(writeScopeContext(t), "warehouse-product-99")
	var output map[string]any
	if err := api.doJSON(ctx, upstream.URL, "/products", http.MethodPost, map[string]any{"name": "PDU"}, &output); err != nil {
		t.Fatal(err)
	}
	if header != "warehouse-product-99" {
		t.Fatalf("Idempotency-Key = %q", header)
	}
	if origin != "MCP/AI" {
		t.Fatalf("X-Cores-Origin = %q", origin)
	}
}

func writeScopeContext(t *testing.T) context.Context {
	return writeScopeContextWithScopes(t, "cores:read", "cores:write")
}

func writeScopeContextWithScopes(t *testing.T, scopes ...string) context.Context {
	t.Helper()
	var result context.Context
	verifier := func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "42", Scopes: scopes, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"username": "tester"}}, nil
	}
	handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		result = r.Context()
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if result == nil {
		t.Fatal("failed to create authenticated context")
	}
	return result
}

func testConfigWithSecret(secret string) config.Config {
	return config.Config{JWTSecret: secret}
}

func TestMutationControlJSONNames(t *testing.T) {
	encoded, err := json.Marshal(JobUpdateInput{MutationControl: MutationControl{DryRun: true, IdempotencyKey: "job-update-42"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"dry_run":true`) || !strings.Contains(string(encoded), `"idempotency_key":"job-update-42"`) {
		t.Fatalf("mutation controls missing from JSON: %s", encoded)
	}
}

func TestMutationAuditAttributesExcludeRawIdempotencyKey(t *testing.T) {
	arguments := json.RawMessage(`{"dry_run":false,"confirm_update":true,"idempotency_key":"secret-retry-key","description":"private value"}`)
	encoded, err := json.Marshal(mutationAuditAttributes(arguments))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"origin","MCP/AI"`) || !strings.Contains(text, `"confirmed",true`) || !strings.Contains(text, `"idempotency_ref"`) {
		t.Fatalf("audit attributes = %s", text)
	}
	if strings.Contains(text, "secret-retry-key") || strings.Contains(text, "private value") {
		t.Fatalf("audit attributes leaked request data: %s", text)
	}
}

func TestCriticalProcurementConfirmationPhrases(t *testing.T) {
	if got := requisitionDecisionPhrase("approved", 17); got != "APPROVE REQUISITION 17" {
		t.Fatalf("approval phrase = %q", got)
	}
	if got := requisitionDecisionPhrase("returned", 17); got != "RETURN REQUISITION 17" {
		t.Fatalf("return phrase = %q", got)
	}
	if got := requisitionDecisionPhrase("unknown", 17); got != "" {
		t.Fatalf("invalid decision phrase = %q", got)
	}
	if got := purchaseOrderReceiptPhrase(23, 42); got != "RECEIVE ORDER 23 LINE 42" {
		t.Fatalf("receipt phrase = %q", got)
	}
	overdelivery := PurchaseOrderReceiptInput{OrderID: 23, LineID: 42, AllowOverdelivery: true}
	if got := purchaseOrderReceiptConfirmation(overdelivery); got != "RECEIVE OVERDELIVERY ORDER 23 LINE 42" {
		t.Fatalf("overdelivery phrase = %q", got)
	}
}

func TestNormalizeSerialInputs(t *testing.T) {
	got := normalizeSerialInputs([]string{" SN-1 ", "sn-1", "", "SN-2"})
	want := []string{"SN-1", "SN-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("serials = %#v, want %#v", got, want)
	}
}

func TestRFC3339ValuePreservesDatabaseVersion(t *testing.T) {
	want := "2026-09-22T08:15:00.123456Z"
	if got := rfc3339Value(want); got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
	value := time.Date(2026, 9, 22, 8, 15, 0, 123456000, time.UTC)
	if got := rfc3339Value(value); got != want {
		t.Fatalf("time version = %q, want %q", got, want)
	}
}

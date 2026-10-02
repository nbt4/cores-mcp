package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestProcurementWorkflowOwnerScopeSchemaRetryAndCurrentRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("workflow-owner-", 3)
	calls := 0
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/mcp/workflows/requisitions/archive" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.IsAdmin || claims.MutationScope != "cores:procurement:archive" {
			t.Fatal("signed requester/archive delegation", err, claims)
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != float64(7) || body["expected_context"] != strings.Repeat("a", 64) {
			t.Fatal(body)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "table", "column", "is_archived"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved lifecycle control", key)
			}
		}
		calls++
		status, out := 200, `{"operation_status":"archived","record":{"id":7,"isArchived":true}}`
		if revoked {
			status, out = 403, `{"error":"Current requester rights revoked"}`
		} else if body["preview"] == true {
			if r.Header.Get("Idempotency-Key") != "" {
				t.Fatal("dry-run receipt")
			}
		} else if !failed {
			failed = true
			status, out = 500, `{"error":"final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	input := ProcurementWorkflowLifecycleInput{MutationControl: MutationControl{DryRun: true}, ID: 7, ExpectedUpdatedAt: "2026-10-03T12:00:00.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "ARCHIVE PROCUREMENT REQUISITION 7 DIGEST", ConfirmChange: true}
	fn := func(ctx context.Context, in ProcurementWorkflowLifecycleInput) (any, []Source, []string, error) {
		return invokeProcurementWorkflowLifecycle(ctx, cfg, "requisitions", "archive", in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "procurement.requisitions.archive", "Archive requisition", input, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	ctx := inventoryTestContext(t, "42", false, "cores:procurement:archive")
	call(ctx, false)
	input.DryRun = false
	input.IdempotencyKey = "workflow-owner-durable-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	revoked = true
	call(ctx, true)
	if calls != 5 {
		t.Fatal("owner authorization bypassed on cached replay", calls)
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:read"), inventoryTestContext(t, "42", false, "cores:procurement:update"), inventoryTestContext(t, "service:test", false, "cores:procurement:archive")} {
		call(denied, true)
	}
	if calls != 5 {
		t.Fatal("unauthorized owner request")
	}
	for _, ns := range []string{"orders", "requisitions"} {
		for _, op := range []string{"archive", "restore"} {
			name := "procurement." + ns + "." + op
			if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:procurement:archive" {
				t.Fatal("scope/registration", name)
			}
		}
		raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement."+ns]))
		for _, field := range []string{"expected_updated_at", "expected_context", "confirmation_text", "archive", "restore", "audit_history"} {
			if !strings.Contains(string(raw), field) {
				t.Fatal(ns, field)
			}
		}
	}
}

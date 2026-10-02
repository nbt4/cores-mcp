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

func TestProcurementMasterOwnerDelegationRetryAndRevocation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("procurement-owner-", 3)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/master-data/products/archive" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.MutationScope != "cores:procurement:archive" {
			t.Fatal("signed real-user archive delegation", err, claims)
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "table", "column", "active"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner field", key)
			}
		}
		body["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, body)
		status, out := 200, `{"operation_status":"archived","record":{"id":7,"active":false}}`
		if revoked {
			status, out = 403, `{"error":"Current administrator required"}`
		} else if body["forwarded_key"] == "procurement-owner-retry" && !failed {
			failed = true
			status, out = 500, `{"error":"forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{ProcurementURL: "http://procurement.invalid", JWTSecret: secret}
	input := ProcurementMasterLifecycleInput{MutationControl: MutationControl{DryRun: true}, ID: 7, ExpectedUpdatedAt: "2026-10-02T10:20:30.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "ARCHIVE PROCUREMENT PRODUCT 7 DIGEST", ConfirmChange: true}
	fn := func(ctx context.Context, in ProcurementMasterLifecycleInput) (any, []Source, []string, error) {
		return invokeProcurementMasterLifecycle(ctx, cfg, "products", "archive", in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "procurement.products.archive", "Archive product", input, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:procurement:archive")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["forwarded_key"] != "" {
		t.Fatal("dry run", calls)
	}
	input.DryRun = false
	input.IdempotencyKey = "procurement-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 4 || calls[3]["expected_context"] != input.ExpectedContext || calls[3]["forwarded_key"] != input.IdempotencyKey {
		t.Fatal("durable owner replay", calls)
	}
	revoked = true
	call(ctx, true)
	if len(calls) != 5 {
		t.Fatal("cache bypassed current owner rights")
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:procurement:archive"), inventoryTestContext(t, "42", true, "cores:procurement:update"), inventoryTestContext(t, "service:test", true, "cores:procurement:archive"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, true)
	}
	if len(calls) != 5 {
		t.Fatal("unauthorized owner request")
	}
}

func TestProcurementMasterLifecycleScopeAndSchema(t *testing.T) {
	for _, ns := range []string{"suppliers", "products", "offers"} {
		for _, op := range []string{"archive", "restore"} {
			name := "procurement." + ns + "." + op
			if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:procurement:archive" {
				t.Fatal(name)
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

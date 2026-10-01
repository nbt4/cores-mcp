package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/config"
)

type inventoryTestRoundTripper func(*http.Request) (*http.Response, error)

func (fn inventoryTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func inventoryTestContext(t *testing.T, user string, admin bool, scopes ...string) context.Context {
	t.Helper()
	var ctx context.Context
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: user, Scopes: scopes, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"username": "counter", "is_admin": admin}}, nil
	}
	handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer inventory-test")
	handler.ServeHTTP(httptest.NewRecorder(), r)
	if ctx == nil {
		t.Fatal("authenticated context missing")
	}
	return ctx
}

func TestWarehouseInventoryApprovalRequiresSeparateScope(t *testing.T) {
	for _, test := range []struct {
		name    string
		scopes  []string
		allowed bool
	}{
		{"read", []string{"cores:read"}, false},
		{"create", []string{"cores:warehouse:create"}, false},
		{"update", []string{"cores:warehouse:update"}, false},
		{"legacy writes", []string{"cores:write"}, false},
		{"approve", []string{"cores:warehouse:approve"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := inventoryTestContext(t, "42", true, test.scopes...)
			_, err := authorizeMutation(ctx, "warehouse.inventory_counts.approve")
			if (err == nil) != test.allowed {
				t.Fatal("incorrect approval authorization", err)
			}
		})
	}
	for _, op := range []string{"create", "update", "set_lines", "review", "return_for_counting", "approve", "cancel", "archive", "restore"} {
		want := "cores:warehouse:update"
		if op == "create" {
			want = "cores:warehouse:create"
		}
		if op == "approve" {
			want = "cores:warehouse:approve"
		}
		if op == "archive" || op == "restore" {
			want = "cores:warehouse:archive"
		}
		name := "warehouse.inventory_counts." + op
		if requiredMutationScope(name) != want || !isMutationTool(name) || !hasDurableWarehouseRetry(name) {
			t.Fatal("missing named mutation protection", name)
		}
	}
}

func TestWarehouseInventoryOwnerForwardingDryRunRetryAndCachedRights(t *testing.T) {
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	calls := []map[string]any{}
	failed := false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/mcp/inventory-counts/approve" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal("unexpected owning API route/origin", r.URL)
		}
		if _, err := r.Cookie("cores_token"); err != nil {
			t.Fatal("actual user delegation missing", err)
		}
		in := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if _, ok := in["idempotency_key"]; ok {
			t.Fatal("key leaked to business payload")
		}
		if _, ok := in["dry_run"]; ok {
			t.Fatal("MCP control leaked to business payload")
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, in)
		status := http.StatusOK
		body := `{"inventory_count":{"count_id":7,"status":"approved"}}`
		if in["forwarded_key"] == "inventory-retry-after-audit-failure" && !failed {
			failed = true
			status = http.StatusInternalServerError
			body = `{"error":"Atomic audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	cfg := config.Config{WarehouseURL: "http://warehouse.invalid", JWTSecret: strings.Repeat("i", 48)}
	ctx := inventoryTestContext(t, "42", true, "cores:warehouse:approve")
	input := WarehouseInventoryInput{CountID: 7, ExpectedUpdatedAt: "2026-10-01T12:00:00.000001Z", ExpectedContext: "exact-context", ConfirmChange: true, ConfirmationText: "APPROVE WAREHOUSE INVENTORY COUNT 7"}
	fn := func(ctx context.Context, in WarehouseInventoryInput) (any, []Source, []string, error) {
		return invokeWarehouseInventory(ctx, cfg, "approve", in, false)
	}
	call := func(ctx context.Context, in WarehouseInventoryInput, wantError bool) {
		t.Helper()
		result, _, err := executeMutationTool(ctx, "warehouse.inventory_counts.approve", "Approve inventory count", in, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatalf("unexpected invocation result %#v %v", result, err)
		}
	}
	input.DryRun = true
	call(ctx, input, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" {
		t.Fatal("dry-run authorized a write", calls)
	}
	input.DryRun = false
	input.IdempotencyKey = "inventory-retry-after-audit-failure"
	call(ctx, input, true)
	call(ctx, input, false)
	call(ctx, input, false)
	if len(calls) != 3 || calls[2]["preview"] != false || calls[2]["expected_context"] != "exact-context" || calls[2]["expected_updated_at"] != input.ExpectedUpdatedAt || calls[2]["forwarded_key"] != input.IdempotencyKey {
		t.Fatal("atomic retry or successful replay broken", calls)
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:warehouse:approve"), inventoryTestContext(t, "service:test", true, "cores:warehouse:approve"), inventoryTestContext(t, "42", true, "cores:warehouse:update"), inventoryTestContext(t, "42", true, "cores:write")} {
		call(denied, input, true)
	}
	if len(calls) != 3 {
		t.Fatal("unauthorized cached action reached owner")
	}
}

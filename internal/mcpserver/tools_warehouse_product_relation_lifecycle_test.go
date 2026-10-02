package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nbt4/cores-mcp/internal/config"
)

func TestWarehouseRelationActionScopesAndDurableProtection(t *testing.T) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		name := "warehouse.product_relations." + op
		scope := "cores:warehouse:update"
		if op == "create" {
			scope = "cores:warehouse:create"
		}
		if op == "archive" || op == "restore" {
			scope = "cores:warehouse:archive"
		}
		if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != scope {
			t.Fatal("missing closed named mutation protection", name)
		}
		if _, err := authorizeMutation(inventoryTestContext(t, "42", true, scope), name); err != nil {
			t.Fatal(err)
		}
		if _, err := authorizeMutation(inventoryTestContext(t, "42", true, "cores:read"), name); err == nil {
			t.Fatal("read scope authorized", name)
		}
	}
}

func TestWarehouseRelationOwnerPreviewRetryAndFreshRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := []map[string]any{}
	failed := false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/admin/mcp/product-relations/update" || r.Method != http.MethodPost || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal("unexpected closed owner route", r.URL)
		}
		if _, err := r.Cookie("cores_token"); err != nil {
			t.Fatal("actual user delegation missing", err)
		}
		in := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"dry_run", "idempotency_key"} {
			if _, ok := in[field]; ok {
				t.Fatal("MCP control leaked", field)
			}
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, in)
		status := 200
		body := `{"operation_status":"updated","relationship":{"relation_id":7,"product_id":1,"dependency_product_id":2}}`
		if in["forwarded_key"] == "relation-retry-atomic-audit" && !failed {
			failed = true
			status = 500
			body = `{"error":"final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	cfg := config.Config{WarehouseURL: "http://warehouse.invalid", JWTSecret: strings.Repeat("r", 48)}
	quantity, notes := 1.25, ""
	in := WarehouseRelationInput{RelationID: 7, DefaultQuantity: &quantity, Notes: &notes, ExpectedUpdatedAt: "2026-10-02T00:10:11.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmChange: true, ConfirmationText: "UPDATE WAREHOUSE PRODUCT RELATION 7"}
	fn := func(ctx context.Context, input WarehouseRelationInput) (any, []Source, []string, error) {
		return invokeWarehouseRelation(ctx, cfg, "update", input, false)
	}
	call := func(ctx context.Context, input WarehouseRelationInput, wantError bool) {
		t.Helper()
		result, _, err := executeMutationTool(ctx, "warehouse.product_relations.update", "Update product relation", input, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatalf("unexpected result %#v %v", result, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:warehouse:update")
	in.DryRun = true
	call(ctx, in, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" {
		t.Fatal("dry-run mutation", calls)
	}
	in.DryRun = false
	in.IdempotencyKey = "relation-retry-atomic-audit"
	call(ctx, in, true)
	call(ctx, in, false)
	call(ctx, in, false)
	if len(calls) != 3 || calls[2]["preview"] != false || calls[2]["notes"] != "" || calls[2]["default_quantity"] != 1.25 || calls[2]["expected_context"] != in.ExpectedContext || calls[2]["expected_updated_at"] != in.ExpectedUpdatedAt || calls[2]["forwarded_key"] != in.IdempotencyKey {
		t.Fatal("fields or durable retry/cache broken", calls)
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:warehouse:update"), inventoryTestContext(t, "service:test", true, "cores:warehouse:update"), inventoryTestContext(t, "42", true, "cores:warehouse:create"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, in, true)
	}
	if len(calls) != 3 {
		t.Fatal("unauthorized cached result reached target")
	}
}

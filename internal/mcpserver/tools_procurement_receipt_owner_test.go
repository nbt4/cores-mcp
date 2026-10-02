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

func TestProcurementGoodsReceiptOwnerDelegationDurableRetryAndRevocation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("goods-receipt-owner-", 3)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/orders/receive" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.MutationScope != "cores:procurement:receive" {
			t.Fatal("wrong signed receipt actor/scope", err, claims)
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "table", "column"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner field", key)
			}
		}
		body["key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, body)
		status, out := 200, `{"operation_status":"received","receipt":{"id":71,"warehouseProductId":7,"putawayTaskId":9,"createdDeviceIds":["DEVICE-1"]}}`
		if body["preview"] == true {
			out = `{"operation_status":"confirmation_required","current":{"warehouse_product":[{"productid":7}],"supplier":[{"id":3}],"target_zone":[{"zone_id":4}]}}`
		}
		if revoked {
			status, out = 403, `{"error":"Current active goods receipt administrator required"}`
		} else if body["key"] == "receipt-owner-retry" && !failed {
			failed = true
			status, out = 500, `{"error":"forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{ProcurementURL: "http://procurement.invalid", JWTSecret: secret}
	input := PurchaseOrderReceiptInput{MutationControl: MutationControl{DryRun: true}, OrderID: 1, LineID: 2, Quantity: 3, SerialNumbers: []string{"S1", "S2", "S3"}, ExpectedContext: strings.Repeat("a", 64), ExpectedUpdatedAt: "2026-10-02T12:00:00.000123Z", ConfirmationText: "RECEIVE ORDER 1 LINE 2 QUANTITY 3 DIGEST", ConfirmReceipt: true}
	fn := func(ctx context.Context, in PurchaseOrderReceiptInput) (any, []Source, []string, error) {
		return invokeProcurementGoodsReceipt(ctx, cfg, in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "procurement.orders.receive", "Receive goods", input, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:procurement:receive")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["key"] != "" {
		t.Fatal("dry-run executed", calls)
	}
	previewCtx, _ := authorizeMutation(ctx, "procurement.orders.receive")
	_, sources, _, err := invokeProcurementGoodsReceipt(previewCtx, cfg, input, true)
	if err != nil || len(sources) != 5 {
		t.Fatal("preview source records", sources, err)
	}
	input.DryRun = false
	input.IdempotencyKey = "receipt-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 5 || calls[4]["expected_context"] != input.ExpectedContext || calls[4]["quantity"] != float64(3) {
		t.Fatal("owner retry/replay", calls)
	}
	revoked = true
	call(ctx, true)
	if len(calls) != 6 {
		t.Fatal("cached result bypassed current owner rights")
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:procurement:receive"), inventoryTestContext(t, "42", true, "cores:write"), inventoryTestContext(t, "42", true, "cores:procurement:approve"), inventoryTestContext(t, "service:test", true, "cores:procurement:receive")} {
		call(denied, true)
	}
	if len(calls) != 6 {
		t.Fatal("unauthorized owner request")
	}
}

func TestProcurementApprovalAndReceiptRequireExplicitSeparatedScopes(t *testing.T) {
	for _, tool := range []string{"procurement.orders.receive", "procurement.orders.transition", "procurement.requisitions.decide"} {
		for _, scope := range []string{"cores:write", "cores:procurement:create", "cores:procurement:update", "cores:read"} {
			if _, err := authorizeMutation(inventoryTestContext(t, "42", true, scope), tool); err == nil {
				t.Fatal("legacy/write scope granted elevated workflow", tool, scope)
			}
		}
		if _, err := authorizeMutation(inventoryTestContext(t, "42", true, requiredMutationScope(tool)), tool); err != nil {
			t.Fatal(tool, err)
		}
		if strings.Contains(mutationPermissionMessage(tool), "or legacy") {
			t.Fatal("misleading elevated workflow consent message", tool)
		}
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement.orders"]))
	for _, field := range []string{"workflow_fields", "receive", "transition", "expected_context", "allow_overdelivery", "serial_numbers", "target_zone_id"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("workflow schema missing", field)
		}
	}
}

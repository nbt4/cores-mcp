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

func TestProcurementApprovalOwnerDelegationRetryAndCurrentRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("approval-owner-", 3)
	failed := map[string]bool{}
	revoked := false
	calls := 0
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || (r.URL.Path != "/api/v1/mcp/approvals/orders" && r.URL.Path != "/api/v1/mcp/approvals/requisitions") {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || !claims.IsAdmin || claims.MutationScope != "cores:procurement:approve" {
			t.Fatal("real administrator/approval delegation", err, claims)
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != float64(7) || body["expected_context"] != strings.Repeat("a", 64) || body["expected_updated_at"] != "2026-10-03T12:00:00.000123Z" {
			t.Fatal(body)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "table", "column", "supplier_id", "notes", "total_cents"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner field", key)
			}
		}
		calls++
		status, out := 200, `{"operation_status":"transitioned","purchase_order":{"id":7,"status":"cancelled"}}`
		if strings.HasSuffix(r.URL.Path, "requisitions") {
			out = `{"operation_status":"decided","requisition":{"id":7,"status":"returned"}}`
		}
		if revoked {
			status, out = 403, `{"error":"Current distinct approval administrator required"}`
		} else if body["preview"] == true {
			if r.Header.Get("Idempotency-Key") != "" || body["confirm_change"] != false {
				t.Fatal("dry-run execution", body)
			}
			out = `{"preview":true,"dependencies":{"products":[{"id":3}],"supplier":[{"id":4}],"receipts":[{"id":9}]}}`
		} else if !failed[r.URL.Path] {
			failed[r.URL.Path] = true
			status, out = 500, `{"error":"forced final approval audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	ctx := inventoryTestContext(t, "42", true, "cores:procurement:approve")
	order := OrderTransitionInput{MutationControl: MutationControl{DryRun: true}, OrderID: 7, Status: "cancelled", Reason: "Reviewed cancel reason", ExpectedUpdatedAt: "2026-10-03T12:00:00.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmTransition: true, ConfirmationText: "CANCEL ORDER 7 CONTEXT"}
	req := RequisitionDecisionInput{MutationControl: MutationControl{DryRun: true}, RequisitionID: 7, Decision: "returned", Note: "Reviewed return reason", ExpectedUpdatedAt: order.ExpectedUpdatedAt, ExpectedContext: order.ExpectedContext, ConfirmDecision: true, ConfirmationText: "RETURN REQUISITION 7 CONTEXT"}
	orderFn := func(ctx context.Context, in OrderTransitionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "orders", in, false)
	}
	reqFn := func(ctx context.Context, in RequisitionDecisionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "requisitions", in, false)
	}
	callOrder := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "procurement.orders.transition", "Transition order", order, orderFn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	callReq := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "procurement.requisitions.decide", "Decide requisition", req, reqFn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	callOrder(ctx, false)
	callReq(ctx, false)
	order.DryRun = false
	order.IdempotencyKey = "approval-order-atomic-retry"
	req.DryRun = false
	req.IdempotencyKey = "approval-requisition-atomic-retry"
	callOrder(ctx, true)
	callOrder(ctx, false)
	callOrder(ctx, false)
	callReq(ctx, true)
	callReq(ctx, false)
	callReq(ctx, false)
	revoked = true
	callOrder(ctx, true)
	callReq(ctx, true)
	if calls != 10 {
		t.Fatal("cached/retry owner invocation", calls)
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:procurement:approve"), inventoryTestContext(t, "42", true, "cores:write"), inventoryTestContext(t, "42", true, "cores:procurement:receive"), inventoryTestContext(t, "service:test", true, "cores:procurement:approve")} {
		callOrder(denied, true)
		callReq(denied, true)
	}
	if calls != 10 {
		t.Fatal("unauthorized owner request", calls)
	}
	for _, entity := range []string{"procurement.orders", "procurement.requisitions"} {
		raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()[entity]))
		for _, field := range []string{"workflow_fields", "expected_context", "confirmation_text"} {
			if !strings.Contains(string(raw), field) {
				t.Fatal(entity, field)
			}
		}
	}
}

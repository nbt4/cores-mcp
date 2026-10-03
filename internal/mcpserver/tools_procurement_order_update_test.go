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

func TestOrderDraftOwnerDelegationRetryAndCachedRevocation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("requester-owner-", 3)
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	calls := 0
	revoked := false
	failed := map[string]bool{}
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		op := strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/order-drafts/")
		if r.Method != "POST" || (op != "create" && op != "update") {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || !claims.IsAdmin || claims.MutationScope != "cores:procurement:"+op {
			t.Fatal("real requester/action", err, claims)
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["expected_context"] != strings.Repeat("a", 64) || body["confirmation_text"] == "" {
			t.Fatal("exact context omitted", body)
		}
		if op == "update" {
			if _, ok := body["lines"]; ok {
				t.Fatal("omitted lines replaced", body)
			}
		}
		for _, key := range []string{"idempotency_key", "dry_run", "requester_id", "total_cents", "table", "column"} {
			if _, ok := body[key]; ok {
				t.Fatal("arbitrary owner field", key)
			}
		}
		calls++
		status := 200
		out := `{"operation_status":"updated","purchase_order":{"id":7}}`
		if body["preview"] == true {
			if r.Header.Get("Idempotency-Key") != "" || body["confirm_change"] != false {
				t.Fatal("dry-run", body)
			}
			out = `{"preview":true,"dependencies":{"products":[{"id":3}],"suppliers":[{"id":4}]}}`
		} else if revoked {
			status = 403
			out = `{"error":"Current requester rights revoked"}`
		} else if !failed[op] {
			failed[op] = true
			status = 500
			out = `{"error":"Forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})

	ctx := inventoryTestContext(t, "42", true, "cores:procurement:update")
	draft := OrderDraftUpdateInput{MutationControl: MutationControl{DryRun: true}, OrderID: 7, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "FINAL ORDER DRAFT CONTEXT", ConfirmUpdate: true}
	fn := func(ctx context.Context, in OrderDraftUpdateInput) (any, []Source, []string, error) {
		return invokeProcurementOrderDraft(ctx, cfg, "update", in, false)
	}
	call := func(ctx context.Context, wantErr bool) {
		t.Helper()
		r, o, e := executeMutationTool(ctx, "procurement.orders.update", "Draft", draft, fn)
		if e != nil || (r != nil && r.IsError) != wantErr {
			t.Fatal(r, o, e)
		}
	}
	call(ctx, false)
	draft.DryRun = false
	draft.IdempotencyKey = "order-draft-owner-update"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	revoked = true
	call(ctx, true)
	revoked = false
	call(inventoryTestContext(t, "42", false, "cores:procurement:update"), true)
	call(inventoryTestContext(t, "42", true, "cores:procurement:receive"), true)
	call(inventoryTestContext(t, "service:test", true, "cores:procurement:update"), true)
	create := PurchaseOrderCreateInput{MutationControl: MutationControl{IdempotencyKey: "order-draft-owner-create"}, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "FINAL ORDER CREATE CONTEXT", ConfirmCreation: true, SupplierID: 3, Lines: []PurchaseOrderLineInput{{ProductID: 4, Description: "Line", Quantity: 2.5, UnitPriceCents: 101}}}
	createFn := func(ctx context.Context, in PurchaseOrderCreateInput) (any, []Source, []string, error) {
		return invokeProcurementOrderDraft(ctx, cfg, "create", in, false)
	}
	createCtx := inventoryTestContext(t, "42", true, "cores:procurement:create")
	for _, wantErr := range []bool{true, false, false} {
		r, o, e := executeMutationTool(createCtx, "procurement.orders.create", "Create", create, createFn)
		if e != nil || (r != nil && r.IsError) != wantErr {
			t.Fatal(r, o, e)
		}
	}
	revoked = true
	r, o, e := executeMutationTool(createCtx, "procurement.orders.create", "Create", create, createFn)
	if e != nil || r == nil || !r.IsError {
		t.Fatal(r, o, e)
	}
	if calls != 9 {
		t.Fatal("cached/retry owner authorization", calls)
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement.orders"]))
	for _, field := range []string{"expected_context", "confirmation_text", "line_id"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing entity schema", field)
		}
	}
}

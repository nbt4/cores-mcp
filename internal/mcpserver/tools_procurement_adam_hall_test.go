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

func TestAdamHallExplicitSendScopeDurableRetryAndCachedRevocation(t *testing.T) {
	for _, operation := range []string{"cart", "send"} {
		t.Run(operation, func(t *testing.T) {
			checkAdamHallOwnerRetry(t, operation)
		})
	}
}

func checkAdamHallOwnerRetry(t *testing.T, operation string) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("requester-owner-", 3)
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	calls := 0
	revoked := false
	failed := map[string]bool{}
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		op := "send"
		if r.Method != "POST" || r.URL.Path != "/api/v1/mcp/orders/adam-hall/"+operation {
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
		for _, key := range []string{"idempotency_key", "dry_run", "requester_id", "total_cents", "table", "column"} {
			if _, ok := body[key]; ok {
				t.Fatal("arbitrary owner field", key)
			}
		}
		if body["checkout_id"] != float64(9) {
			t.Fatal("reviewed checkout identity missing", body)
		}
		calls++
		status := 200
		out := `{"operation_status":"sent","purchase_order":{"id":7},"checkout_id":9,"submission_id":11}`
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

	ctx := inventoryTestContext(t, "42", true, "cores:procurement:send")
	draft := AdamHallSendInput{MutationControl: MutationControl{DryRun: true}, AdamHallOrderControl: AdamHallOrderControl{OrderID: 7, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "FINAL REVIEWED SUPPLIER CONTEXT"}, CheckoutID: 9, ConfirmSend: true}
	fn := func(ctx context.Context, in AdamHallSendInput) (any, []Source, []string, error) {
		return invokeProcurementAdamHall(ctx, cfg, operation, in.AdamHallOrderControl, in.CheckoutID, in.ConfirmSend, in.DryRun)
	}
	call := func(ctx context.Context, wantErr bool) {
		t.Helper()
		name := "procurement.orders.send_adam_hall"
		if operation == "cart" {
			name = "procurement.orders.build_adam_hall_cart"
		}
		r, o, e := executeMutationTool(ctx, name, "Draft", draft, fn)
		if e != nil || (r != nil && r.IsError) != wantErr {
			t.Fatal(r, o, e)
		}
		expectedWarning := "may already exist"
		if operation == "cart" {
			expectedWarning = "cart may already have changed"
		}
		if wantErr && strings.Contains(o.Summary, name+" failed:") && !strings.Contains(strings.Join(o.Warnings, " "), expectedWarning) {
			t.Fatal("uncertain external effect warning omitted", o)
		}
	}
	call(ctx, false)
	draft.DryRun = false
	draft.IdempotencyKey = "adam-hall-" + operation + "-owner-key"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	revoked = true
	call(ctx, true)
	revoked = false
	call(inventoryTestContext(t, "42", false, "cores:procurement:send"), true)
	call(inventoryTestContext(t, "42", true, "cores:procurement:receive"), true)
	call(inventoryTestContext(t, "42", true, "cores:write"), true)
	call(inventoryTestContext(t, "42", true, "cores:procurement:submit"), true)
	call(inventoryTestContext(t, "service:test", true, "cores:procurement:send"), true)
	if calls != 5 {
		t.Fatal("cached/retry owner authorization", calls)
	}
	changed := draft
	changed.OrderID++
	name, expectedWarning := "procurement.orders.send_adam_hall", "may already exist"
	if operation == "cart" {
		name, expectedWarning = "procurement.orders.build_adam_hall_cart", "cart may already have changed"
	}
	r, out, err := executeMutationTool(ctx, name, "Draft", changed, fn)
	if err != nil || r == nil || !r.IsError || calls != 5 || !strings.Contains(strings.Join(out.Warnings, " "), expectedWarning) {
		t.Fatal("changed replay payload hides original supplier effect", r, out, err, calls)
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement.orders"]))
	for _, field := range []string{"expected_context", "confirmation_text", "confirm_send", "confirm_cart", "checkout_id"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing entity schema", field)
		}
	}
}

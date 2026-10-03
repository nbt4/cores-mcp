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

func TestAmazonSendExplicitScopeOwnerRetryAndCachedRevocation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("requester-owner-", 3)
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	calls := 0
	revoked := false
	failed := map[string]bool{}
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		op := "send"
		if r.Method != "POST" || r.URL.Path != "/api/v1/mcp/orders/send-amazon" {
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

	ctx := inventoryTestContext(t, "42", true, "cores:procurement:send")
	draft := AmazonOrderSendInput{MutationControl: MutationControl{DryRun: true}, OrderID: 7, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "FINAL ORDER DRAFT CONTEXT", ConfirmSend: true}
	fn := func(ctx context.Context, in AmazonOrderSendInput) (any, []Source, []string, error) {
		return invokeProcurementAmazonSend(ctx, cfg, in, false)
	}
	call := func(ctx context.Context, wantErr bool) {
		t.Helper()
		r, o, e := executeMutationTool(ctx, "procurement.orders.send_amazon", "Draft", draft, fn)
		if e != nil || (r != nil && r.IsError) != wantErr {
			t.Fatal(r, o, e)
		}
		if wantErr && strings.Contains(o.Summary, "send_amazon failed:") && !strings.Contains(strings.Join(o.Warnings, " "), "may already exist") {
			t.Fatal("uncertain external effect warning omitted", o)
		}
	}
	call(ctx, false)
	draft.DryRun = false
	draft.IdempotencyKey = "amazon-send-owner-key"
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
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement.orders"]))
	for _, field := range []string{"expected_context", "confirmation_text", "confirm_send"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing entity schema", field)
		}
	}
}

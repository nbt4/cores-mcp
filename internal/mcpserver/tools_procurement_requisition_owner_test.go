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

func TestRequisitionOwnerDelegationRetryAndCachedRevocation(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("requester-owner-", 3)
	cfg := config.Config{JWTSecret: secret, ProcurementURL: "http://procurement.invalid"}
	calls := 0
	revoked := false
	failed := map[string]bool{}
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		op := strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/requisitions/")
		if r.Method != "POST" || (op != "create" && op != "update" && op != "submit") {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.IsAdmin || claims.MutationScope != "cores:procurement:"+op {
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
		for _, key := range []string{"idempotency_key", "dry_run", "requester_id", "status", "total_cents", "table", "column"} {
			if _, ok := body[key]; ok {
				t.Fatal("arbitrary owner field", key)
			}
		}
		calls++
		status := 200
		out := `{"operation_status":"updated","requisition":{"id":7}}`
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
	for _, op := range []string{"create", "update", "submit"} {
		ctx := inventoryTestContext(t, "42", false, "cores:procurement:"+op)
		name := "procurement.requisitions." + op
		draft := RequisitionDraftInput{MutationControl: MutationControl{DryRun: true}, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "FINAL DRAFT CONTEXT", ConfirmCreation: op == "create", ConfirmUpdate: op == "update"}
		if op == "update" {
			draft.RequisitionID = 7
		}
		submit := RequisitionSubmitInput{MutationControl: MutationControl{DryRun: true}, RequisitionID: 7, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "SUBMIT REQUISITION 7 CONTEXT", ConfirmSubmit: true}
		fn := func(ctx context.Context, in RequisitionDraftInput) (any, []Source, []string, error) {
			return invokeProcurementRequisitionDraft(ctx, cfg, op, in, false)
		}
		submitFn := func(ctx context.Context, in RequisitionSubmitInput) (any, []Source, []string, error) {
			return invokeProcurementRequisitionDraft(ctx, cfg, op, in, false)
		}
		call := func(ctx context.Context, wantErr bool) {
			t.Helper()
			var rejected bool
			if op == "submit" {
				r, o, e := executeMutationTool(ctx, name, "Submit", submit, submitFn)
				rejected = r != nil && r.IsError
				if e != nil || rejected != wantErr {
					t.Fatal(r, o, e)
				}
			} else {
				r, o, e := executeMutationTool(ctx, name, "Draft", draft, fn)
				rejected = r != nil && r.IsError
				if e != nil || rejected != wantErr {
					t.Fatal(r, o, e)
				}
			}
		}
		call(ctx, false)
		draft.DryRun = false
		submit.DryRun = false
		draft.IdempotencyKey = "requester-owner-" + op
		submit.IdempotencyKey = draft.IdempotencyKey
		call(ctx, true)
		call(ctx, false)
		call(ctx, false)
		revoked = true
		call(ctx, true)
		revoked = false
		call(inventoryTestContext(t, "42", false, "cores:procurement:receive"), true)
		call(inventoryTestContext(t, "service:test", false, "cores:procurement:"+op), true)
	}
	if calls != 15 {
		t.Fatal("cached/retry owner authorization", calls)
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["procurement.requisitions"]))
	for _, field := range []string{"expected_context", "confirmation_text", "line_id", "allow_duplicate", "submit"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing entity schema", field)
		}
	}
}

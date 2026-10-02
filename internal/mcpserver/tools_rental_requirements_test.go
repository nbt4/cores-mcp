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

func TestRequirementOwnerDelegationDryRunRetryAndCurrentRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("requirement-test-", 4)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/requirements/update" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.MutationScope != "cores:rental:update" {
			t.Fatal("wrong signed delegation", err, claims)
		}
		body := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "confirm_update", "job_query", "product_query", "position_quantity", "entity", "table", "column"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner input", key)
			}
		}
		body["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, body)
		status, out := 200, `{"operation_status":"updated","requirement":{"requirement_id":7,"job_id":3,"product_id":4,"quantity":2,"manual_quantity":0,"position_quantity":2}}`
		if revoked {
			status, out = 403, `{"error":"Current rental administrator rights required"}`
		} else if body["forwarded_key"] == "requirement-owner-retry" && !failed {
			failed = true
			status, out = 500, `{"error":"forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: secret}
	zero := int64(0)
	in := RequirementUpdateInput{MutationControl: MutationControl{DryRun: true}, RentalRequirementPreviewControl: RentalRequirementPreviewControl{ExpectedUpdatedAt: "2026-10-02T10:20:30.000123Z", ExpectedJobUpdatedAt: "2026-10-02T10:20:31.000124Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "UPDATE RENTAL REQUIREMENT 7 PREVIEW-DIGEST"}, RequirementID: 7, ManualQuantity: &zero, ConfirmUpdate: true}
	fn := func(ctx context.Context, in RequirementUpdateInput) (any, []Source, []string, error) {
		return invokeRentalRequirement(ctx, cfg, nil, "update", in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "rental.requirements.update", "Update requirement", in, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:rental:update")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" || !in.ConfirmUpdate {
		t.Fatal("dry-run confirmation", calls)
	}
	in.DryRun = false
	in.IdempotencyKey = "requirement-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 4 || calls[3]["manual_quantity"] != float64(0) || calls[3]["expected_job_updated_at"] != in.ExpectedJobUpdatedAt || calls[3]["forwarded_key"] != in.IdempotencyKey {
		t.Fatal("retry/replay/complete controls", calls)
	}
	revoked = true
	call(ctx, true)
	if len(calls) != 5 {
		t.Fatal("cached owner rights bypass")
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:rental:update"), inventoryTestContext(t, "42", true, "cores:rental:create"), inventoryTestContext(t, "service:test", true, "cores:rental:update"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, true)
	}
	if len(calls) != 5 {
		t.Fatal("unauthorized request reached owner")
	}
}

func TestRequirementLifecycleScopesAndCompleteSchema(t *testing.T) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		name := "rental.requirements." + op
		action := op
		if op == "restore" {
			action = "archive"
		}
		if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:"+action {
			t.Fatal(name)
		}
	}
	encoded, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.requirements"]))
	for _, field := range []string{"manual_quantity", "expected_updated_at", "expected_job_updated_at", "expected_context", "confirmation_text", "archive", "restore", "audit_history"} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("missing schema", field)
		}
	}
}

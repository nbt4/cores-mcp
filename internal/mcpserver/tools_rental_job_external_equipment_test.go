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

func TestExternalRentalAssignmentDelegationDryRunRetryAndRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("rental-assignment-test-", 3)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/jobs/external-equipment-create" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		tok, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid || claims.UserID != 42 || claims.MutationScope != "cores:rental:create" || !claims.FinancialScope {
			t.Fatal("wrong signed delegation", err, claims)
		}
		body := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "job_query", "equipment_query", "unit_price", "total_cost", "product_id", "entity", "table"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner input", key)
			}
		}
		body["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, body)
		status, out := 201, `{"operation_status":"created","assignment":{"job_id":3,"equipment_id":4,"quantity":2,"days_used":3,"total_cost":60},"position":{"position_id":9,"position_type":"rental","rental_equipment_id":4,"unit_price":50}}`
		if revoked {
			status, out = 403, `{"error":"Current rental administrator rights required"}`
		} else if body["forwarded_key"] == "external-rental-owner-retry" && !failed {
			failed = true
			status, out = 500, `{"error":"forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: secret}
	quantity, days := int64(2), int64(3)
	in := RentalJobExternalEquipmentInput{MutationControl: MutationControl{DryRun: true}, JobID: 3, EquipmentID: 4, Quantity: &quantity, DaysUsed: &days, ExpectedJobUpdatedAt: "2026-10-07T10:20:31.000124Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "CREATE RENTAL JOB EXTERNAL EQUIPMENT 3/4 PREVIEW-DIGEST", ConfirmChange: true}
	fn := func(ctx context.Context, in RentalJobExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalJobExternalEquipment(ctx, cfg, nil, in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "rental.job_external_equipment.create", "Assign rental", in, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
		if !wantError && !in.DryRun && (out.AsOf == "" || len(out.Sources) != 4 || out.Sources[0].ID != "3/4" || out.Sources[3].Entity != "job_position" || out.Sources[3].ID != "9") {
			t.Fatal("missing timestamp/source identities", out)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:rental:create", "cores:rental:financial")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" || !in.ConfirmChange {
		t.Fatal("dry-run confirmation", calls)
	}
	in.DryRun = false
	in.IdempotencyKey = "external-rental-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 4 || calls[3]["quantity"] != float64(2) || calls[3]["days_used"] != float64(3) || calls[3]["expected_job_updated_at"] != in.ExpectedJobUpdatedAt || calls[3]["forwarded_key"] != in.IdempotencyKey {
		t.Fatal("retry/replay/controls", calls)
	}
	in.RepairExisting = true
	in.IdempotencyKey = "external-rental-explicit-repair"
	call(ctx, false)
	if calls[len(calls)-1]["repair_existing"] != true {
		t.Fatal("repair intent was dropped")
	}
	revoked = true
	call(ctx, true)
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:rental:create", "cores:rental:financial"), inventoryTestContext(t, "42", true, "cores:rental:update", "cores:rental:financial"), inventoryTestContext(t, "service:test", true, "cores:rental:create", "cores:rental:financial"), inventoryTestContext(t, "42", true, "cores:rental:create"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, true)
	}
	if len(calls) != 6 {
		t.Fatal("unauthorized request reached owner", len(calls))
	}
	// A query is never used to pick an identity during confirmed replay.
	in.JobID = 0
	in.JobQuery = "Ambiguous job"
	in.IdempotencyKey = "external-rental-unresolved"
	call(ctx, true)
	if len(calls) != 6 {
		t.Fatal("unresolved execution reached owner")
	}
}

func TestExternalRentalAssignmentScopesAndCompleteSchema(t *testing.T) {
	name := "rental.job_external_equipment.create"
	if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:create" {
		t.Fatal("missing mutation guard", name)
	}
	encoded, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.job_external_equipment"]))
	for _, field := range []string{"job_id", "equipment_id", "quantity", "days_used", "repair_existing", "expected_job_updated_at", "expected_context", "confirmation_text", "prepare_create", "idempotency_key"} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("missing schema", field)
		}
	}
	for _, tool := range []string{name, "rental.job_external_equipment.prepare_create"} {
		scopes := positionOAuthScopes(tool)
		if !containsString(scopes, "cores:read") || !containsString(scopes, "cores:rental:financial") || !containsString(scopes, "cores:rental:create") {
			t.Fatal("missing OAuth discovery scopes", tool, scopes)
		}
	}
}

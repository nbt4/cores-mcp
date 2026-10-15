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

func TestExternalRentalCatalogDelegationDryRunRetryAndRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("catalog-test-", 4)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/external-equipment/create" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.UserID != 42 || claims.MutationScope != "cores:rental:create" || !claims.FinancialScope {
			t.Fatal("wrong signed delegation", err, claims)
		}
		body := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "product_id", "job_id", "table", "entity"} {
			if _, ok := body[key]; ok {
				t.Fatal("unapproved owner input", key)
			}
		}
		if body["rental_price"] != float64(0) || body["customer_price"] != 25.50 || body["category"] != "Audio" || body["supplier"] != "Synthetic supplier" {
			t.Fatal("lost catalog fields", body)
		}
		body["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, body)
		status, out := 201, `{"operation_status":"created","equipment":{"equipment_id":7,"name":"Synthetic rental"}}`
		if revoked {
			status, out = 403, `{"error":"Current administrator rights required"}`
		} else if body["forwarded_key"] == "catalog-owner-retry" && !failed {
			failed = true
			status, out = 500, `{"error":"forced audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(out)), Request: r}, nil
	})
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: secret}
	zero, price := 0.0, 25.50
	in := RentalExternalEquipmentInput{MutationControl: MutationControl{DryRun: true}, Name: "Synthetic rental", Category: "Audio", Supplier: "Synthetic supplier", SupplierID: 3, RentalPrice: &zero, CustomerPrice: &price, ConfirmChange: true, ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "CREATE RENTAL EXTERNAL EQUIPMENT preview"}
	fn := func(ctx context.Context, in RentalExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalExternalEquipment(ctx, cfg, in, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, out, err := executeMutationTool(ctx, "rental.external_equipment.create", "Create rental", in, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatal(result, out, err)
		}
		if !wantError && !in.DryRun && (out.AsOf == "" || len(out.Sources) != 2 || out.Sources[0].ID != "7" || out.Sources[1].ID != "3") {
			t.Fatal("missing sources/time", out)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:rental:create", "cores:rental:financial")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" {
		t.Fatal("dry-run mutated", calls)
	}
	in.DryRun = false
	in.IdempotencyKey = "catalog-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 4 || calls[3]["forwarded_key"] != in.IdempotencyKey {
		t.Fatal("owner retry/replay missing", calls)
	}
	revoked = true
	call(ctx, true)
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:rental:create", "cores:rental:financial"), inventoryTestContext(t, "42", true, "cores:rental:update", "cores:rental:financial"), inventoryTestContext(t, "service:test", true, "cores:rental:create", "cores:rental:financial"), inventoryTestContext(t, "42", true, "cores:rental:create"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, true)
	}
	if len(calls) != 5 {
		t.Fatal("unauthorized request reached owner", len(calls))
	}
}

func TestExternalRentalCatalogScopesAndSchema(t *testing.T) {
	name := "rental.external_equipment.create"
	if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:create" {
		t.Fatal("missing guards")
	}
	encoded, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.external_equipment"]))
	for _, field := range []string{"name", "category", "supplier", "supplier_id", "rental_price", "customer_price", "expected_context", "confirmation_text", "prepare_create", "idempotency_key"} {
		if !strings.Contains(string(encoded), field) {
			t.Fatal("missing schema", field)
		}
	}
	for _, tool := range []string{name, "rental.external_equipment.prepare_create"} {
		scopes := positionOAuthScopes(tool)
		for _, s := range []string{"cores:read", "cores:rental:financial", "cores:rental:create"} {
			if !containsString(scopes, s) {
				t.Fatal("missing OAuth scope", tool, s)
			}
		}
	}
}

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

func TestRentalMasterOwnerDelegationDryRunRetryAndRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("rental-test-", 4)
	calls := []map[string]any{}
	failed := false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/customers/update" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.UserID != 42 || claims.MutationScope != "cores:rental:update" {
			t.Fatal("unsigned/wrong user/action delegation", err, claims.UserID, claims.MutationScope)
		}
		in := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "operation", "entity", "table", "column"} {
			if _, ok := in[key]; ok {
				t.Fatal("unapproved owner input", key)
			}
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, in)
		status, body := 200, `{"operation_status":"updated","record":{"id":7,"phone":null}}`
		if in["forwarded_key"] == "rental-master-retry-owner" && !failed {
			failed = true
			status = 500
			body = `{"error":"forced final audit rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: secret}
	phone := ""
	in := RentalCustomerInput{RentalMasterControl: RentalMasterControl{ID: 7, ExpectedUpdatedAt: "2026-10-02T08:01:02.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmChange: true, ConfirmationText: "UPDATE RENTAL CUSTOMER 7 PREVIEW-DIGEST"}, Phone: &phone}
	fn := func(ctx context.Context, input RentalCustomerInput) (any, []Source, []string, error) {
		return invokeRentalMaster(ctx, cfg, "customers", "update", input, false)
	}
	call := func(ctx context.Context, input RentalCustomerInput, wantError bool) {
		t.Helper()
		result, _, err := executeMutationTool(ctx, "rental.customers.update", "Update customer", input, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatalf("%#v %v", result, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:rental:update")
	in.DryRun = true
	call(ctx, in, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" || !in.ConfirmChange {
		t.Fatal("embedded confirmation dry-run mutated or leaked", calls, in)
	}
	in.DryRun = false
	in.IdempotencyKey = "rental-master-retry-owner"
	call(ctx, in, true)
	call(ctx, in, false)
	call(ctx, in, false)
	if len(calls) != 3 || calls[2]["phone"] != "" || calls[2]["preview"] != false || calls[2]["forwarded_key"] != in.IdempotencyKey || calls[2]["expected_context"] != in.ExpectedContext {
		t.Fatal("durable retry/fields/cache", calls)
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:rental:update"), inventoryTestContext(t, "service:test", true, "cores:rental:update"), inventoryTestContext(t, "42", true, "cores:rental:create"), inventoryTestContext(t, "42", true, "cores:read")} {
		call(denied, in, true)
	}
	if len(calls) != 3 {
		t.Fatal("cached result bypassed rights")
	}
}

func TestRentalMasterScopesSchemasAndArchivedResolution(t *testing.T) {
	for _, entity := range []string{"customers", "venues"} {
		for _, op := range []string{"create", "update", "archive", "restore"} {
			name := "rental." + entity + "." + op
			action := op
			if action == "restore" {
				action = "archive"
			}
			if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:"+action {
				t.Fatal(name)
			}
		}
		schema := writableEntitySchemas()["rental."+entity]
		rendered := renderWritableEntitySchema(schema)
		raw, _ := json.Marshal(rendered)
		for _, field := range []string{"expected_context", "expected_updated_at", "confirm_change", "confirmation_text", "notes", "email"} {
			if !strings.Contains(string(raw), field) {
				t.Fatal("missing complete schema", entity, field)
			}
		}
	}
	status, selected := classifyMasterCandidates(rankMasterCandidates([]map[string]any{{"id": int64(1), "name": "Existing organization", "is_archived": true}}, "Existing organization", 10))
	if status != "restoration_required" || selected != nil {
		t.Fatal(status, selected)
	}
}

func TestEmbeddedPointerConfirmationDryRunPreservesCaller(t *testing.T) {
	type EmbeddedControl struct{ ConfirmChange bool }
	type Input struct {
		*EmbeddedControl
		MutationControl
	}
	input := Input{EmbeddedControl: &EmbeddedControl{ConfirmChange: true}, MutationControl: MutationControl{DryRun: true}}
	prepared, err := prepareWriteInvocation(context.Background(), "rental.customers.update", input)
	if err != nil || prepared.Confirmed || prepared.Input.ConfirmChange || !input.ConfirmChange || prepared.Input.EmbeddedControl == input.EmbeddedControl {
		t.Fatal("nested dry-run modified caller or retained confirmation", err)
	}
}

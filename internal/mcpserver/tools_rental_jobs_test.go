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

func TestRentalJobOwnerDelegationFinancialDryRunRetryAndReplayRights(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	secret := strings.Repeat("job-test-", 6)
	calls := []map[string]any{}
	failed, revoked := false, false
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/mcp/jobs/update" || r.Method != "POST" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Fatal(r.URL)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Fatal(err)
		}
		claims := &suiteServiceClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.UserID != 42 || claims.MutationScope != "cores:rental:update" || !claims.FinancialScope {
			t.Fatal("wrong signed delegation", err, claims)
		}
		in := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "confirm_update", "customer_query", "job_query", "final_revenue", "entity", "table", "column"} {
			if _, ok := in[key]; ok {
				t.Fatal("unapproved payload", key)
			}
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		calls = append(calls, in)
		status, body := 200, `{"operation_status":"updated","job":{"job_id":7,"revenue":80}}`
		if revoked {
			status, body = 403, `{"error":"Current rental administrator rights required"}`
		} else if in["forwarded_key"] == "rental-job-owner-retry" && !failed {
			failed = true
			status, body = 500, `{"error":"forced atomic rollback"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: secret}
	discount := 20.0
	multiply := false
	in := JobUpdateInput{MutationControl: MutationControl{DryRun: true}, RentalJobPreviewControl: RentalJobPreviewControl{ExpectedUpdatedAt: "2026-10-02T08:01:02.000123Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "UPDATE RENTAL JOB 7 PREVIEW-DIGEST"}, JobID: 7, Discount: &discount, MultiplyByDays: &multiply, ConfirmUpdate: true}
	fn := func(ctx context.Context, input JobUpdateInput) (any, []Source, []string, error) {
		return invokeRentalJob(ctx, cfg, nil, "update", input, false)
	}
	call := func(ctx context.Context, wantError bool) {
		t.Helper()
		result, _, err := executeMutationTool(ctx, "rental.jobs.update", "Update job", in, fn)
		if err != nil || (result != nil && result.IsError) != wantError {
			t.Fatalf("%#v %v", result, err)
		}
	}
	ctx := inventoryTestContext(t, "42", true, "cores:rental:update", "cores:rental:financial")
	call(ctx, false)
	if calls[0]["preview"] != true || calls[0]["confirm_change"] == true || calls[0]["forwarded_key"] != "" || !in.ConfirmUpdate {
		t.Fatal("dry-run confirmation", calls)
	}
	in.DryRun = false
	in.IdempotencyKey = "rental-job-owner-retry"
	call(ctx, true)
	call(ctx, false)
	call(ctx, false)
	if len(calls) != 4 || calls[3]["forwarded_key"] != in.IdempotencyKey || calls[3]["multiply_by_days"] != false || calls[3]["expected_context"] != in.ExpectedContext {
		t.Fatal("durable retry and fresh owner replay", calls)
	}
	revoked = true
	call(ctx, true)
	if len(calls) != 5 {
		t.Fatal("revoked owner bypassed by cache")
	}
	for _, denied := range []context.Context{inventoryTestContext(t, "42", false, "cores:rental:update", "cores:rental:financial"), inventoryTestContext(t, "42", true, "cores:rental:update"), inventoryTestContext(t, "42", true, "cores:rental:create", "cores:rental:financial"), inventoryTestContext(t, "service:test", true, "cores:rental:update", "cores:rental:financial")} {
		call(denied, true)
	}
	if len(calls) != 5 {
		t.Fatal("denied scope reached owner")
	}
}

func TestRentalJobLifecycleScopeAndCompleteSchemas(t *testing.T) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		name := "rental.jobs." + op
		action := op
		if op == "restore" {
			action = "archive"
		}
		if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:"+action {
			t.Fatal(name)
		}
	}
	schema := writableEntitySchemas()["rental.jobs"]
	raw, _ := json.Marshal(renderWritableEntitySchema(schema))
	encoded := string(raw)
	for _, field := range []string{"discount", "discount_type", "multiply_by_days", "prices_include_tax", "clear_fields", "expected_context", "expected_updated_at", "confirmation_text", "prepare_archive", "restore", "audit_history"} {
		if !strings.Contains(encoded, field) {
			t.Fatal("incomplete schema", field)
		}
	}
}

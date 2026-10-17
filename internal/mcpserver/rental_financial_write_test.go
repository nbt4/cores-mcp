package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestRentalFinancialWritesAcrossHTTP(t *testing.T) {
	cfg := config.Config{EnableWrites: true, PublicURL: "https://cores.example"}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scopeSets := map[string][]string{
		"write":           {"cores:read", "cores:write"},
		"other-financial": {"cores:read", "cores:write", warehouseFinancialScope, procurementFinancialScope},
		"financial-only":  {"cores:read", rentalFinancialScope},
	}
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "7", Scopes: scopeSets[token], Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	endpoint := httptest.NewServer(auth.RequireBearerToken(verify, nil)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})))
	defer endpoint.Close()
	for token := range scopeSets {
		session, err := mcp.NewClient(&mcp.Implementation{Name: "rental-financial-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		count := 0
		for name := range mutationTools {
			if !rentalFinancialWriteTool(name) {
				continue
			}
			count++
			cut := strings.LastIndex(name, ".")
			for _, tool := range []string{name, name[:cut] + ".prepare_" + name[cut+1:]} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
				if err != nil || result == nil {
					t.Fatal(tool, err)
				}
				challenge := prettyJSON(result.Meta["mcp/www_authenticate"])
				if !result.IsError || !strings.Contains(challenge, "insufficient_scope") || !strings.Contains(challenge, rentalFinancialScope) || token == "financial-only" && !strings.Contains(challenge, requiredMutationScope(name)) {
					t.Fatalf("%s/%s: %s", token, tool, prettyJSON(result))
				}
				if scopes := positionOAuthScopes(tool); !containsString(scopes, rentalFinancialScope) || !containsString(scopes, requiredMutationScope(name)) {
					t.Fatal(tool, scopes)
				}
			}
		}
		if got := requiredMutationScope("rental.jobs.assign_device"); got != "cores:rental:update" {
			t.Fatal("existing assignment scope changed", got)
		}
		if count != 12 {
			t.Fatal("financial workflow coverage changed", count)
		}
		for _, input := range []map[string]any{{"revenue": 0}, {"discount": 0}, {"discount_type": "amount"}, {"multiply_by_days": false}, {"prices_include_tax": false}} {
			for _, tool := range []string{"rental.jobs.prepare_create", "rental.jobs.create", "rental.jobs.prepare_update", "rental.jobs.update"} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: input})
				if err != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), rentalFinancialScope) {
					t.Fatal(token, tool, result, err)
				}
			}
		}
	}
}

func TestRentalFinancialGuardPrecedesExecutionAndReplay(t *testing.T) {
	for name := range mutationTools {
		if !rentalFinancialWriteTool(name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			type financialInput struct {
				MutationControl
				ConfirmChange bool `json:"confirm_change"`
			}
			input := financialInput{MutationControl: MutationControl{IdempotencyKey: "financial-rental-" + name}, ConfirmChange: true}
			fn := func(context.Context, financialInput) (any, []Source, []string, error) {
				calls++
				return map[string]any{"rental_price": 917.25}, nil, nil, nil
			}
			granted := inventoryTestContext(t, "867", true, "cores:write", rentalFinancialScope)
			result, _, err := executeMutationTool(granted, name, "write", input, fn)
			if err != nil || result != nil && result.IsError || calls != 1 {
				t.Fatal(result, err, calls)
			}
			for _, scopes := range [][]string{{"cores:write"}, {requiredMutationScope(name)}, {requiredMutationScope(name), warehouseFinancialScope, procurementFinancialScope}} {
				ctx := inventoryTestContext(t, "867", true, scopes...)
				result, output, err := executeMutationTool(ctx, name, "write", input, fn)
				if err != nil || result == nil || !result.IsError || output.Data != nil || calls != 1 {
					t.Fatal("financial receipt leaked", result, output, err, calls)
				}
			}
		})
	}
	ctx := inventoryTestContext(t, "7", true, "cores:rental:create")
	if _, err := prepareJobDeviceAssign(ctx, nil, JobDeviceAssignInput{}); err == nil || !strings.Contains(err.Error(), rentalFinancialScope) {
		t.Fatal("assignment reached SQL before financial guard", err)
	}
	for _, input := range []any{map[string]any{"revenue": 0}, map[string]any{"multiply_by_days": false}, json.RawMessage(`{"prices_include_tax":false}`), json.RawMessage(`invalid`)} {
		if err := requireRentalJobFinancialInput(ctx, input); err == nil {
			t.Fatal("commercial input accepted", input)
		}
	}
}

func TestOperationalJobFinancialProjectionAndReplay(t *testing.T) {
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/jobs/update" {
			t.Error(r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"job":{"job_id":7,"description":"Operational","revenue":917.25,"discount":17,"prices_include_tax":false},"current":{"final_revenue":900.25},"draft":{"multiply_by_days":false},"diff":{"revenue":{"before":800,"after":917.25},"description":{"before":"Old","after":"Operational"}},"expected_context":"opaque-context","required_confirmation_text":"exact-confirmation"}`)
	}))
	defer owner.Close()
	cfg := config.Config{RentalURL: owner.URL, JWTSecret: strings.Repeat("synthetic-", 6)}
	in := JobUpdateInput{JobID: 7, Description: "Operational", ConfirmUpdate: true, MutationControl: MutationControl{IdempotencyKey: "operational-financial-projection"}}
	fn := func(ctx context.Context, in JobUpdateInput) (any, []Source, []string, error) {
		return invokeRentalJob(ctx, cfg, nil, "update", in, false)
	}
	for _, scopes := range [][]string{{"cores:rental:update", rentalFinancialScope}, {"cores:rental:update"}, {"cores:write", warehouseFinancialScope, procurementFinancialScope}, {"cores:rental:update", rentalFinancialScope}} {
		ctx := inventoryTestContext(t, "868", true, scopes...)
		result, output, err := executeMutationTool(ctx, "rental.jobs.update", "write", in, fn)
		if err != nil || result != nil && result.IsError {
			t.Fatal(result, err)
		}
		raw := prettyJSON(output.Data)
		if hasFinancialScope(ctx, rentalFinancialScope) != strings.Contains(raw, "917.25") {
			t.Fatal("wrong financial projection", raw)
		}
		if !hasFinancialScope(ctx, rentalFinancialScope) {
			for _, key := range []string{"revenue", "final_revenue", "discount", "prices_include_tax", "multiply_by_days"} {
				if strings.Contains(raw, `"`+key+`"`) {
					t.Fatal("financial field leaked", key, raw)
				}
			}
		}
		for _, preserved := range []string{"opaque-context", "exact-confirmation", "Operational", "description"} {
			if !strings.Contains(raw, preserved) {
				t.Fatal("operational contract altered", raw)
			}
		}
	}
}

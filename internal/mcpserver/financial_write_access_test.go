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

func TestProcurementFinancialWriteScopesAcrossHTTP(t *testing.T) {
	cfg := config.Config{EnableWrites: true, PublicURL: "https://cores.example"}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scopeSets := map[string][]string{
		"write":           {"cores:read", "cores:write"},
		"other-financial": {"cores:read", "cores:write", rentalFinancialScope, warehouseFinancialScope},
		"financial-only":  {"cores:read", procurementFinancialScope},
	}
	verify := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "7", Scopes: scopeSets[token], Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	endpoint := httptest.NewServer(auth.RequireBearerToken(verify, nil)(handler))
	defer endpoint.Close()
	for token := range scopeSets {
		session, err := mcp.NewClient(&mcp.Implementation{Name: "financial-write-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		count := 0
		for name := range mutationTools {
			if !procurementFinancialWriteTool(name) {
				continue
			}
			count++
			cut := strings.LastIndex(name, ".")
			for _, tool := range []string{name, name[:cut] + ".prepare_" + name[cut+1:]} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
				if err != nil {
					t.Fatal(tool, err)
				}
				challenge := prettyJSON(result.Meta["mcp/www_authenticate"])
				if !result.IsError || !strings.Contains(challenge, "insufficient_scope") || !strings.Contains(challenge, procurementFinancialScope) || token == "financial-only" && !strings.Contains(challenge, requiredMutationScope(name)) {
					t.Fatalf("%s/%s: %s", token, tool, prettyJSON(result))
				}
			}
		}
		if count != 21 {
			t.Fatal("financial workflow coverage changed", count)
		}
		for _, input := range []map[string]any{{"product_url": "https://example.invalid/shop"}, {"supplier_id": 3}, {"price_cents": 1}} {
			for _, tool := range []string{"procurement.products.prepare_create", "procurement.products.create"} {
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: input})
				if err != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), procurementFinancialScope) {
					t.Fatal("conditional product financial challenge", token, tool, result, err)
				}
			}
		}
	}
	// Legacy broad writes never replace explicitly separated approve/receive/send.
	scopeSets["legacy-financial"] = []string{"cores:read", "cores:write", procurementFinancialScope}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "financial-write-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: "legacy-financial"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, name := range []string{"procurement.orders.transition", "procurement.requisitions.decide", "procurement.orders.receive", "procurement.orders.send_amazon", "procurement.orders.send_adam_hall", "procurement.orders.build_adam_hall_cart", "procurement.orders.reconcile_submission"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), requiredMutationScope(name)) {
			t.Fatal("dedicated action missing from consent challenge", name, result, err)
		}
	}

}

func TestProcurementFinancialGrantPrecedesCachedReceipt(t *testing.T) {
	input := OfferCreateInput{MutationControl: MutationControl{IdempotencyKey: "financial-replay-original-success"}, ConfirmCreation: true}
	calls := 0
	fn := func(context.Context, OfferCreateInput) (any, []Source, []string, error) {
		calls++
		return map[string]any{"price_cents": 12345}, nil, nil, nil
	}
	granted := inventoryTestContext(t, "741", true, "cores:procurement:create", procurementFinancialScope)
	result, output, err := executeMutationTool(granted, "procurement.offers.create", "create", input, fn)
	if err != nil || result != nil && result.IsError || !strings.Contains(prettyJSON(output.Data), "12345") {
		t.Fatal(result, output, err)
	}
	for _, scopes := range [][]string{{"cores:write"}, {"cores:procurement:create"}, {"cores:procurement:create", rentalFinancialScope, warehouseFinancialScope}} {
		ctx := inventoryTestContext(t, "741", true, scopes...)
		result, output, err = executeMutationTool(ctx, "procurement.offers.create", "create", input, fn)
		if err != nil || result == nil || !result.IsError || output.Data != nil || strings.Contains(prettyJSON(output), "12345") || calls != 1 {
			t.Fatal("cached financial receipt leaked", result, output, err, calls)
		}
	}
	result, output, err = executeMutationTool(granted, "procurement.offers.create", "create", input, fn)
	if err != nil || result != nil && result.IsError || !strings.Contains(prettyJSON(output.Data), "12345") || calls != 1 {
		t.Fatal("original valid receipt changed", result, output, err, calls)
	}
}

func TestOperationalProductPreparationNeedsNoFinancialGrant(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "operational-fixture", Version: "1"}, nil)
	called := 0
	addWritePreparationTool(server, "procurement.products.prepare_create", "Prepare product", "Synthetic operational product", func(context.Context, ProductCreateInput) (any, []Source, []string, error) {
		called++
		return map[string]any{"name": "Synthetic product"}, nil, nil, nil
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := inventoryTestContext(t, "7", true, "cores:procurement:create")
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "procurement.products.prepare_create", Arguments: map[string]any{"sku": "TEST", "name": "Synthetic"}})
	if err != nil || result.IsError || called != 1 {
		t.Fatal("operational product blocked", result, err, called)
	}
	for _, input := range []ProductCreateInput{{ProductURL: "https://example.invalid"}, {SupplierID: 3}, {PriceCents: 1}} {
		if _, err := prepareProductCreate(ctx, nil, nil, config.Config{}, input); err == nil || !strings.Contains(err.Error(), procurementFinancialScope) {
			t.Fatal("price preparation reached store or scraper", err)
		}
	}
	raw, _ := json.Marshal(ProductCreateInput{})
	if procurementProductFinancialInput(json.RawMessage(raw)) {
		t.Fatal("empty operational input requires finance")
	}
}

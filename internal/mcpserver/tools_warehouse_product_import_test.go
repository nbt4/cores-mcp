package mcpserver

import (
	"context"
	"encoding/json"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWarehouseProductBulkHTTPControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/products/bulk-create" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Error("wrong business route or origin")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if _, ok := in["idempotency_key"]; ok {
			t.Error("MCP controls leaked to target body")
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Error("missing signed requester")
		} else {
			claims := jwt.MapClaims{}
			_, err = jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(strings.Repeat("k", 48)), nil }, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || claims["uid"] != float64(11) || claims["mcp_scope"] != "cores:warehouse:create" {
				t.Error("wrong signed real-user/create delegation", err)
			}
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "operation_status": "created", "products": []map[string]any{{"product_id": 1}}})
	}))
	defer upstream.Close()
	cfg := config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL}
	server := mcp.NewServer(&mcp.Implementation{Name: "bulk-controls", Version: "1"}, nil)
	registerWarehouseProductImportTools(server, cfg)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"cores:read", "cores:warehouse:create"}
		admin := true
		user := "11"
		if raw == "read" {
			scopes = []string{"cores:read"}
		}
		if raw == "nonadmin" {
			admin = false
		}
		if raw == "machine" {
			user = "service:test"
		}
		return &auth.TokenInfo{UserID: user, Scopes: scopes, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
	}
	gateway := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(transport))
	defer gateway.Close()
	connect := func(subject string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "bulk-client", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: gateway.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: subject}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	member := connect("write")
	call := func(session *mcp.ClientSession, name string, in map[string]any, wantError bool) {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: in})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: %#v", name, result)
		}
	}
	call(member, "warehouse.products.prepare_bulk_create", map[string]any{"products": []map[string]any{{"name": "Product"}}, "confirm_creation": true}, false)
	call(member, "warehouse.products.bulk_create", map[string]any{"products": []map[string]any{{"name": "Product"}}}, false)
	call(member, "warehouse.products.bulk_create", map[string]any{"products": []map[string]any{{"name": "Product"}}, "confirm_creation": true, "dry_run": true}, false)
	call(member, "warehouse.products.bulk_create", map[string]any{"products": []map[string]any{{"name": "Product"}}, "confirm_creation": true}, true)
	args := map[string]any{"products": []map[string]any{{"name": "Product"}}, "confirm_creation": true, "idempotency_key": "bulk-http-once"}
	call(member, "warehouse.products.bulk_create", args, false)
	call(member, "warehouse.products.bulk_create", args, false)
	for _, subject := range []string{"read", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.products.bulk_create", map[string]any{"products": []map[string]any{{"name": "Product"}}, "confirm_creation": true, "idempotency_key": "bulk-http-denied"}, true)
	}
	if requiredMutationScope("warehouse.products.bulk_create") != "cores:warehouse:create" {
		t.Fatal("wrong bulk scope")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("unexpected upstream executions: %#v", calls)
	}
	for i := 0; i < 3; i++ {
		if calls[i]["preview"] != true {
			t.Fatal("preview/dry-run mutated", calls)
		}
	}
	if calls[3]["preview"] != false || calls[3]["forwarded_key"] != "bulk-http-once" || calls[4]["forwarded_key"] != "bulk-http-once" {
		t.Fatal("missing durable replay delegation", calls)
	}
}

func TestWarehouseProductPageMergeFreezesExplicitValues(t *testing.T) {
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v1/admin/mcp/products/extract-url" {
			t.Error("wrong extraction route")
		}
		var input map[string]any
		json.NewDecoder(r.Body).Decode(&input)
		if len(input) != 1 || input["product_url"] != "https://manufacturer.example/node" {
			t.Error("non-business request", input)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"draft": map[string]any{"name": "Page name", "manufacturer": "MA Lighting", "brand": "grandMA3", "model": "Node 4", "weight": 1.5, "attributes": map[string]string{"ports": "4", "color": "page"}}})
	}))
	defer upstream.Close()
	cfg := config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL}
	var ctx context.Context
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "11", Scopes: []string{"cores:warehouse:create"}, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer test")
	auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), request)
	if ctx == nil {
		t.Fatal("missing authenticated context")
	}
	ctx = withMutationPermission(ctx, "cores:warehouse:create")
	in := WarehouseProductCreateInput{ProductURL: "https://manufacturer.example/node", Name: "User name", Attributes: map[string]any{"color": "user"}, ConfirmCreation: true, MutationControl: MutationControl{IdempotencyKey: "must-not-reuse"}}
	out, err := mergeWarehouseProductPage(ctx, newCoreAPIClient(cfg), cfg, in)
	if err != nil || out.Name != "User name" || out.ManufacturerName != "MA Lighting" || out.ModelNumber != "Node 4" || out.Weight == nil || *out.Weight != 1.5 || out.Attributes["ports"] != "4" || out.Attributes["color"] != "user" {
		t.Fatalf("merge %#v %v", out, err)
	}
	if out.ProductURL != "" || out.ConfirmCreation || out.IdempotencyKey != "" || out.CreateManufacturer || out.CreateBrand {
		t.Fatal("extraction auto-approved or retained mutable URL", out)
	}
	if _, err = mergeWarehouseProductPage(ctx, newCoreAPIClient(cfg), cfg, out); err != nil || requests != 1 {
		t.Fatal("frozen draft fetched again", err, requests)
	}
}

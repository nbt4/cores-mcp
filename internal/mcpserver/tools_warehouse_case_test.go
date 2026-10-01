package mcpserver

import (
	"context"
	"encoding/json"
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

func TestWarehouseCaseHTTPControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/cases/create" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Error("wrong business route or origin")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if _, ok := in["idempotency_key"]; ok {
			t.Error("MCP controls leaked to target body")
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "case": map[string]any{"case_id": 42}})
	}))
	defer upstream.Close()
	cfg := config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL}
	server := mcp.NewServer(&mcp.Implementation{Name: "case-controls", Version: "1"}, nil)
	registerWarehouseCaseTools(server, cfg, nil)
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
		client := mcp.NewClient(&mcp.Implementation{Name: "case-client", Version: "1"}, nil)
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
	call(member, "warehouse.cases.prepare_create", map[string]any{"name": "Fixture", "confirm_change": true}, false)
	call(member, "warehouse.cases.create", map[string]any{"name": "Fixture"}, false)
	call(member, "warehouse.cases.create", map[string]any{"name": "Fixture", "confirm_change": true, "dry_run": true}, false)
	call(member, "warehouse.cases.create", map[string]any{"name": "Fixture", "confirm_change": true}, true)
	args := map[string]any{"name": "Fixture", "confirm_change": true, "idempotency_key": "case-http-once"}
	call(member, "warehouse.cases.create", args, false)
	call(member, "warehouse.cases.create", args, false)
	for _, subject := range []string{"read", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.cases.create", map[string]any{"name": "Fixture", "confirm_change": true, "idempotency_key": "case-http-denied"}, true)
	}
	call(member, "warehouse.cases.archive", map[string]any{"case_id": 42}, true)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("unexpected upstream executions: %#v", calls)
	}
	for i := 0; i < 3; i++ {
		if calls[i]["preview"] != true {
			t.Fatal("preview/dry-run mutated", calls)
		}
	}
	if calls[3]["preview"] != false || calls[3]["forwarded_key"] != "case-http-once" {
		t.Fatal("missing durable replay delegation", calls)
	}
}

func TestWarehouseCaseScopes(t *testing.T) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		want := op
		if op == "restore" {
			want = "archive"
		}
		if requiredMutationScope("warehouse.cases."+op) != "cores:warehouse:"+want {
			t.Fatal(op)
		}
	}
}

func TestEntityDiscoveryIncludesControlsAndNestedOrderLines(t *testing.T) {
	fields := renderWritableEntitySchema(writableEntitySchemas()["procurement.orders"])["fields"].([]map[string]any)
	found := map[string]map[string]any{}
	for _, field := range fields {
		found[field["name"].(string)] = field
	}
	for _, name := range []string{"dry_run", "idempotency_key", "confirm_creation", "lines"} {
		if found[name] == nil {
			t.Fatalf("missing field %s", name)
		}
	}
	lines, ok := found["lines"]["item_fields"].([]map[string]any)
	if !ok || len(lines) == 0 {
		t.Fatal("missing line schema", found["lines"])
	}
	nested := map[string]bool{}
	for _, field := range lines {
		nested[field["name"].(string)] = true
	}
	for _, name := range []string{"product_id", "quantity", "unit_price_cents"} {
		if !nested[name] {
			t.Fatalf("missing order line field %s", name)
		}
	}
	for _, entity := range []string{"rental.jobs", "rental.requirements"} {
		if renderWritableEntitySchema(writableEntitySchemas()[entity])["update_fields"] == nil {
			t.Fatal("missing update schema", entity)
		}
	}
}

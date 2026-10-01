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

func TestWarehouseCategoryLifecycleHTTPControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	failed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/category/archive" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
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
		if in["forwarded_key"] == "category-http-retry" && !failed {
			failed = true
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":"final audit rollback"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "category": map[string]any{"id": "1"}})
	}))
	defer upstream.Close()
	cfg := config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL}
	server := mcp.NewServer(&mcp.Implementation{Name: "master-controls", Version: "1"}, nil)
	registerWarehouseCategoryLifecycleTools(server, cfg)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"cores:read", "cores:warehouse:archive"}
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
		client := mcp.NewClient(&mcp.Implementation{Name: "master-client", Version: "1"}, nil)
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
	call(member, "warehouse.categories.prepare_archive", map[string]any{"id": "1", "confirm_lifecycle": true}, false)
	call(member, "warehouse.categories.archive", map[string]any{"id": "1"}, false)
	call(member, "warehouse.categories.archive", map[string]any{"id": "1", "confirm_lifecycle": true, "dry_run": true}, false)
	call(member, "warehouse.categories.archive", map[string]any{"id": "1", "confirm_lifecycle": true}, true)
	args := map[string]any{"id": "1", "confirm_lifecycle": true, "expected_updated_at": "2026-10-02T00:01:02.000003Z", "expected_dependencies": strings.Repeat("a", 64), "confirmation_text": "ARCHIVE WAREHOUSE CATEGORY 1", "idempotency_key": "category-http-once"}
	call(member, "warehouse.categories.archive", args, false)
	call(member, "warehouse.categories.archive", args, false)
	args["idempotency_key"] = "category-http-retry"
	call(member, "warehouse.categories.archive", args, true)
	call(member, "warehouse.categories.archive", args, false)
	call(member, "warehouse.categories.archive", args, false)
	for _, subject := range []string{"read", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.categories.archive", args, true)
		call(connect(subject), "warehouse.categories.archive", map[string]any{"id": "1", "confirm_lifecycle": true, "idempotency_key": "category-http-denied"}, true)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 6 {
		t.Fatalf("unexpected upstream executions: %#v", calls)
	}
	for i := 0; i < 3; i++ {
		if calls[i]["preview"] != true {
			t.Fatal("preview/dry-run mutated", calls)
		}
	}
	if calls[3]["preview"] != false || calls[3]["forwarded_key"] != "category-http-once" || calls[3]["expected_dependencies"] != strings.Repeat("a", 64) || calls[3]["expected_updated_at"] != "2026-10-02T00:01:02.000003Z" || calls[3]["confirmation_text"] != "ARCHIVE WAREHOUSE CATEGORY 1" {
		t.Fatal("missing durable replay delegation", calls)
	}
}

func TestWarehouseCategoryLifecycleScopes(t *testing.T) {
	for _, entity := range []string{"categories", "subcategories", "third_categories"} {
		for _, op := range []string{"archive", "restore"} {
			if name := "warehouse." + entity + "." + op; requiredMutationScope(name) != "cores:warehouse:archive" || !isMutationTool(name) || !hasDurableWarehouseRetry(name) {
				t.Fatal(entity, op)
			}
		}
	}
}

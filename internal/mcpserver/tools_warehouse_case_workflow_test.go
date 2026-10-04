package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestWarehouseCaseWorkflowHTTPControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/case-workflows/seal" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
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
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "case": map[string]any{"case_id": 42, "destination_zone_id": 1}})
	}))
	defer upstream.Close()
	cfg := config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL}
	server := mcp.NewServer(&mcp.Implementation{Name: "case-controls", Version: "1"}, nil)
	registerWarehouseCaseWorkflowTools(server, cfg)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"cores:read", "cores:warehouse:update"}
		admin := true
		user := "11"
		if raw == "create-only" {
			scopes = []string{"cores:read", "cores:warehouse:create"}
		}
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
	call(member, "warehouse.case_workflows.prepare_seal", map[string]any{"case_id": 1, "accept_incomplete_template": true, "confirm_change": true}, false)
	call(member, "warehouse.case_workflows.seal", map[string]any{"case_id": 1, "accept_incomplete_template": true}, false)
	call(member, "warehouse.case_workflows.seal", map[string]any{"case_id": 1, "accept_incomplete_template": true, "confirm_change": true, "dry_run": true}, false)
	call(member, "warehouse.case_workflows.seal", map[string]any{"case_id": 1, "accept_incomplete_template": true, "confirm_change": true}, true)
	args := map[string]any{"case_id": 1, "accept_incomplete_template": true, "confirm_change": true, "idempotency_key": "case-http-once"}
	call(member, "warehouse.case_workflows.seal", args, false)
	call(member, "warehouse.case_workflows.seal", args, false)
	for _, subject := range []string{"read", "create-only", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.case_workflows.seal", map[string]any{"case_id": 1, "accept_incomplete_template": true, "confirm_change": true, "idempotency_key": "case-http-denied"}, true)
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
	if calls[3]["preview"] != false || calls[3]["forwarded_key"] != "case-http-once" {
		t.Fatal("missing durable replay delegation", calls)
	}
}

func TestWarehouseCaseWorkflowScopesAndSchema(t *testing.T) {
	for _, op := range []string{"seal", "pack_product", "pack_case", "unseal", "unpack_product", "unpack_case", "unpack_all"} {
		name := "warehouse.case_workflows." + op
		if requiredMutationScope(name) != "cores:warehouse:update" || !hasDurableWarehouseRetry(name) {
			t.Fatal(name)
		}
	}
	schema := writableEntitySchemas()["warehouse.case_workflows"]
	if len(schema.Operations) != 13 {
		t.Fatal(schema)
	}
	fields := renderWritableEntitySchema(schema)["fields"].([]map[string]any)
	names := map[string]bool{}
	for _, f := range fields {
		names[f["name"].(string)] = true
	}
	for _, name := range []string{"case_id", "job_id", "return_mode", "accept_incomplete_template", "inspection_passed", "destination_zone_id", "expected_context", "expected_updated_at", "confirmation_text", "confirm_change", "idempotency_key", "dry_run"} {
		if !names[name] {
			t.Fatal(name)
		}
	}
}

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

func TestWarehouseTaskGuidedHTTPControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	failureCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/tasks/create" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Error("wrong owning route/origin")
		}
		in := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if _, ok := in["idempotency_key"]; ok {
			t.Error("MCP controls leaked")
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		mu.Lock()
		calls = append(calls, in)
		fail := false
		if r.Header.Get("Idempotency-Key") == "task-http-error-retry" {
			failureCalls++
			fail = failureCalls == 1
		}
		mu.Unlock()
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "transient owner transport failure"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "warehouse_task": map[string]any{"task_id": 42}})
	}))
	defer upstream.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "task-controls", Version: "1"}, nil)
	registerWarehouseTaskTools(server, config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"cores:read", "cores:warehouse:create"}
		admin := true
		subject := "11"
		if raw == "read" {
			scopes = []string{"cores:read"}
		}
		if raw == "nonadmin" {
			admin = false
		}
		if raw == "machine" {
			subject = "service:test"
		}
		return &auth.TokenInfo{UserID: subject, Scopes: scopes, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
	}
	gateway := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(transport))
	defer gateway.Close()
	connect := func(subject string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "task-client", Version: "1"}, nil)
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
	call(member, "warehouse.tasks.prepare_create", map[string]any{"task_type": "move", "confirm_creation": true}, false)
	call(member, "warehouse.tasks.create", map[string]any{"task_type": "move"}, false)
	call(member, "warehouse.tasks.create", map[string]any{"task_type": "move", "confirm_creation": true, "dry_run": true}, false)
	call(member, "warehouse.tasks.create", map[string]any{"task_type": "move", "confirm_creation": true}, true)
	args := map[string]any{"task_type": "move", "priority": 0, "confirm_creation": true, "idempotency_key": "task-http-once", "expected_references": map[string]string{"device_id": "exact-reference-version"}}
	call(member, "warehouse.tasks.create", args, false)
	call(member, "warehouse.tasks.create", args, false)
	for _, subject := range []string{"read", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.tasks.create", args, true)
	}
	call(member, "warehouse.tasks.archive", map[string]any{"task_id": 42}, true)
	args["idempotency_key"] = "task-http-error-retry"
	call(member, "warehouse.tasks.create", args, true)
	call(member, "warehouse.tasks.create", args, false)
	call(member, "warehouse.tasks.create", args, false)
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 6 {
		t.Fatal("unexpected upstream writes/replays", calls)
	}
	for i := 0; i < 3; i++ {
		if calls[i]["preview"] != true {
			t.Fatal("preview/dryrun mutated", calls)
		}
	}
	if calls[3]["preview"] != false || calls[3]["priority"] != float64(0) || calls[3]["forwarded_key"] != "task-http-once" {
		t.Fatal("creation fields or durable forwarding lost", calls)
	}
}

func TestWarehouseTaskOperationScopes(t *testing.T) {
	for _, op := range []string{"create", "update", "start", "complete", "cancel", "reopen", "archive", "restore"} {
		action := "update"
		if op == "create" {
			action = "create"
		}
		if op == "archive" || op == "restore" {
			action = "archive"
		}
		if requiredMutationScope("warehouse.tasks."+op) != "cores:warehouse:"+action {
			t.Fatal(op)
		}
	}
}

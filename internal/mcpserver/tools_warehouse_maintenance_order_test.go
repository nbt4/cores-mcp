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

func TestWarehouseMaintenanceOrderFinancialAndReplayControls(t *testing.T) {
	var mu sync.Mutex
	calls := []map[string]any{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/mcp/maintenance-orders/update" || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Error("wrong closed route/origin")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if _, ok := in["idempotency_key"]; ok {
			t.Error("controls leaked")
		}
		in["forwarded_key"] = r.Header.Get("Idempotency-Key")
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"preview": in["preview"], "order": map[string]any{"order_id": 42, "cost_amount": "123.45"}, "diff": map[string]any{"cost_amount": map[string]any{"before": "99.99", "after": "123.45"}}, "legacy_defect": map[string]any{"repair_cost": 99.99}})
	}))
	defer upstream.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "maintenance-controls", Version: "1"}, nil)
	registerWarehouseMaintenanceOrderTools(server, config.Config{JWTSecret: strings.Repeat("k", 48), WarehouseURL: upstream.URL})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		scopes := []string{"cores:read", "cores:warehouse:update"}
		admin := true
		user := "11"
		if raw == "finance" {
			scopes = append(scopes, warehouseFinancialScope)
		}
		if raw == "legacy" {
			scopes = []string{"cores:read", "cores:write"}
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
		client := mcp.NewClient(&mcp.Implementation{Name: "work-client", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: gateway.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: subject}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	member, finance, legacy := connect("write"), connect("finance"), connect("legacy")
	call := func(session *mcp.ClientSession, name string, in map[string]any, wantError, wantCost bool) {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: in})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: %#v", name, result)
		}
		raw, _ := json.Marshal(result)
		if !wantError && strings.Contains(string(raw), "123.45") != wantCost {
			t.Fatal("financial result visibility wrong", string(raw))
		}
		if !wantCost && strings.Contains(string(raw), "99.99") {
			t.Fatal("nested/legacy cost leaked", string(raw))
		}
	}
	args := map[string]any{"order_id": 42, "title": "New title", "confirm_change": true, "idempotency_key": "work-shared-replay"}
	call(finance, "warehouse.maintenance_orders.update", args, false, true)
	call(finance, "warehouse.maintenance_orders.update", args, false, true) // no repeat upstream
	call(member, "warehouse.maintenance_orders.update", args, false, false) // same actual user/key, reduced scope
	args["cost_amount"] = "123.45"
	args["idempotency_key"] = "work-finance-replay"
	call(finance, "warehouse.maintenance_orders.update", args, false, true)
	call(member, "warehouse.maintenance_orders.update", args, true, false)
	call(legacy, "warehouse.maintenance_orders.update", args, true, false) // legacy write is not financial
	delete(args, "cost_amount")
	args["clear_fields"] = []string{"cost_amount"}
	call(member, "warehouse.maintenance_orders.prepare_update", args, true, false)
	delete(args, "clear_fields")
	delete(args, "idempotency_key")
	args["dry_run"] = true
	call(member, "warehouse.maintenance_orders.update", args, false, false)
	delete(args, "dry_run")
	delete(args, "confirm_change")
	call(member, "warehouse.maintenance_orders.update", args, false, false)
	for _, subject := range []string{"read", "nonadmin", "machine"} {
		call(connect(subject), "warehouse.maintenance_orders.update", map[string]any{"order_id": 42, "confirm_change": true, "idempotency_key": "work-shared-replay"}, true, false)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 5 {
		t.Fatal("unexpected upstream writes/replays", calls)
	}
	for _, i := range []int{3, 4} {
		if calls[i]["preview"] != true {
			t.Fatal("dry-run/unconfirmed wrote", calls)
		}
	}
}

func TestWarehouseMaintenanceOrderScopes(t *testing.T) {
	for _, entity := range []string{"maintenance_orders", "defects"} {
		for _, op := range maintenanceOrderOperations {
			action := "update"
			if op == "create" {
				action = "create"
			}
			if op == "archive" || op == "restore" {
				action = "archive"
			}
			if requiredMutationScope("warehouse."+entity+"."+op) != "cores:warehouse:"+action {
				t.Fatal(entity, op)
			}
		}
	}
	if err := checkMaintenanceFinancialQuery(context.Background(), []string{"cost"}); err == nil {
		t.Fatal("flexible financial projection/filter/sort/aggregate is not gated")
	}
}

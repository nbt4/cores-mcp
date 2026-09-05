package mcpserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

//go:embed testdata/planner.sql
var plannerFixture string

// This test exercises the real HTTP MCP transport and PostgreSQL queries with
// two users on one pooled connection, including membership removal and aggregates.
func TestPlannerAccessAcrossMCPTools(t *testing.T) {
	raw := os.Getenv("CORES_MCP_AUTH_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("CORES_MCP_AUTH_TEST_DATABASE_URL is not set")
	}
	target, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(target.Path, "_test") {
		t.Fatal("use an isolated database with a name ending in _test")
	}
	adminDB, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := fmt.Sprintf("mcp_access_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
	options := target.Query()
	options.Set("search_path", schema)
	target.RawQuery = options.Encode()
	database, err := sql.Open("pgx", target.String())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(plannerFixture); err != nil {
		t.Fatal(err)
	}
	const ownPlan = "00000000-0000-0000-0000-000000000001"
	const otherPlan = "00000000-0000-0000-0000-000000000002"
	_, err = database.Exec(`
 INSERT INTO planner_plans (id,name,description) VALUES
 ('00000000-0000-0000-0000-000000000001','VISIBLE plan','visible'),
 ('00000000-0000-0000-0000-000000000002','SECRET plan','secret');
 INSERT INTO planner_members (plan_id,user_id) VALUES
 ('00000000-0000-0000-0000-000000000001','7'),
 ('00000000-0000-0000-0000-000000000002','8');
 INSERT INTO planner_tasks (id,plan_id,title,due_date) VALUES
 ('10000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000001','VISIBLE task',now()-interval '1 day'),
 ('10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000002','SECRET task',now()-interval '1 day');
 INSERT INTO planner_sprints (plan_id,name) SELECT id,name FROM planner_plans;
 INSERT INTO planner_goals (plan_id,title) SELECT id,name FROM planner_plans;
 INSERT INTO planner_dependencies (predecessor_id,successor_id) VALUES
 ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');`)
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "access-test", Version: "1"}, nil)
	repository := store.New(database, 5*time.Second, 200)
	registerPlannerTools(server, repository)
	registerQueryTools(server, repository)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: raw, Scopes: []string{"cores:read"}, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	httpServer := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(transport))
	defer httpServer.Close()
	connect := func(subject string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: subject}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	member, machine := connect("7"), connect("service:automation")
	call := func(session *mcp.ClientSession, name string, arguments any) string {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("%s failed: %s", name, encoded)
		}
		return string(encoded)
	}
	for _, name := range []string{"planner.plans.search", "planner.tasks.search", "planner.tasks.overdue", "planner.workload.summary", "planner.sprints.list", "planner.goals.list", "planner.dependencies.list"} {
		t.Run(name, func(t *testing.T) {
			result := call(member, name, map[string]any{})
			if strings.Contains(result, "SECRET") {
				t.Fatalf("private plan leaked through %s", name)
			}
			if name != "planner.workload.summary" && name != "planner.dependencies.list" && !strings.Contains(result, "VISIBLE") {
				t.Fatalf("member lost access through %s: %s", name, result)
			}
		})
	}
	if result := call(member, "planner.plans.get", map[string]any{"id": ownPlan}); !strings.Contains(result, "VISIBLE") {
		t.Fatal("own plan missing")
	}
	if result := call(member, "planner.plans.get", map[string]any{"id": otherPlan}); strings.Contains(result, "SECRET") {
		t.Fatal("foreign plan readable by ID")
	}
	records := map[string]any{"queries": []map[string]any{{"alias": "plans", "entity": "planner.plans"}, {"alias": "tasks", "entity": "planner.tasks"}}}
	result := call(member, "cores.query.records", records)
	if strings.Contains(result, "SECRET") || !strings.Contains(result, "VISIBLE") {
		t.Fatalf("query catalog did not preserve membership: %s", result)
	}
	result = call(member, "cores.query.aggregate", map[string]any{"entity": "planner.plans", "group_by": []string{"name"}})
	if strings.Contains(result, "SECRET") || !strings.Contains(result, "VISIBLE") {
		t.Fatalf("aggregate leaked or omitted plans: %s", result)
	}
	// A machine request immediately following a user request reuses the database
	// connection, but must not inherit its identity.
	if result := call(machine, "cores.query.records", records); strings.Contains(result, "VISIBLE") || strings.Contains(result, "SECRET") {
		t.Fatal("machine inherited user access")
	}
	if _, err := database.Exec("DELETE FROM planner_members WHERE user_id='7'"); err != nil {
		t.Fatal(err)
	}
	if result := call(member, "cores.query.records", records); strings.Contains(result, "VISIBLE") {
		t.Fatal("revoked membership survived")
	}
}

type accessTestTransport struct{ subject string }

func (transport accessTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.subject)
	return http.DefaultTransport.RoundTrip(request)
}

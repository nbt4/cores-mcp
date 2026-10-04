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
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

//go:embed testdata/procurement_read_access.sql
var procurementReadAccessFixture string

// Real MCP HTTP requests alternate identities on one pooled PG connection.
// Access applies before search, pagination, joins and aggregation, and current
// database rights override deliberately stale administrator token claims.
func TestProcurementRequisitionReadAccessAcrossMCPTools(t *testing.T) {
	raw := os.Getenv("CORES_MCP_PROCUREMENT_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set CORES_MCP_PROCUREMENT_TEST_DATABASE_URL to an isolated _test PostgreSQL database")
	}
	target, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(target.Path, "_test") {
		t.Fatal("an isolated _test database is required")
	}
	adminDB, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := fmt.Sprintf("mcp_procurement_read_%d", time.Now().UnixNano())
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
	if _, err := database.Exec(procurementReadAccessFixture); err != nil {
		t.Fatal(err)
	}
	repository := store.New(database, 5*time.Second, 200)
	server := mcp.NewServer(&mcp.Implementation{Name: "procurement-access-test", Version: "1"}, nil)
	registerProcurementTools(server, repository)
	registerQueryTools(server, repository)
	registerSuiteTools(server, config.Config{}, repository)
	registerCrossCoreTools(server, repository)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verifier := func(_ context.Context, subject string, _ *http.Request) (*auth.TokenInfo, error) {
		if subject == "invalid-identity" {
			subject = "41' OR TRUE--"
		}
		return &auth.TokenInfo{UserID: subject, Scopes: []string{"cores:read"}, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
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
	member, other, admin, machine, invalid := connect("41"), connect("42"), connect("43"), connect("service:automation"), connect("invalid-identity")
	call := func(session *mcp.ClientSession, name string, args any) Output {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("%s: %s", name, encoded)
		}
		var output Output
		if err := json.Unmarshal(encoded, &output); err != nil {
			t.Fatal(err)
		}
		return output
	}
	records := map[string]any{"queries": []map[string]any{{"alias": "requests", "entity": "procurement.requisitions"}, {"alias": "lines", "entity": "procurement.requisition_lines"}}, "joins": []map[string]any{{"alias": "request_lines", "left": "requests", "right": "lines", "relationship": "procurement.requisition_lines"}}}
	cases := []struct {
		name string
		args any
	}{
		{"procurement.requisitions.list", map[string]any{"limit": 1}},
		{"procurement.products.get", map[string]any{"id": "1"}},
		{"cores.query.records", records},
		{"cores.query.aggregate", map[string]any{"entity": "procurement.requisitions", "group_by": []string{"title"}}},
		{"cores.search", map[string]any{"query": ""}},
		{"cores.activity.recent", map[string]any{"from": time.Now().AddDate(-1, 0, 0).Format("2006-01-02"), "to": time.Now().AddDate(1, 0, 0).Format("2006-01-02")}},
	}
	for _, tc := range cases {
		{
			own := prettyJSON(call(member, tc.name, tc.args).Data)
			if !strings.Contains(own, "VISIBLE-41") || strings.Contains(own, "SECRET-42") {
				t.Fatalf("member scope lost or foreign record leaked: %s", own)
			}
			foreign := prettyJSON(call(other, tc.name, tc.args).Data)
			if strings.Contains(foreign, "VISIBLE-41") || !strings.Contains(foreign, "SECRET-42") {
				t.Fatalf("pooled member identity leaked: %s", foreign)
			}
			all := prettyJSON(call(admin, tc.name, tc.args).Data)
			// list's explicit limit=1 deliberately selects the newest record, so SQL
			// permission filtering must precede that limit for the first member.
			if !strings.Contains(all, "SECRET-42") || (tc.name != "procurement.requisitions.list" && !strings.Contains(all, "VISIBLE-41")) {
				t.Fatalf("administrator lost access: %s", all)
			}
			for _, session := range []*mcp.ClientSession{machine, invalid} {
				empty := prettyJSON(call(session, tc.name, tc.args).Data)
				if strings.Contains(empty, "VISIBLE-41") || strings.Contains(empty, "SECRET-42") {
					t.Fatalf("invalid identity inherited access: %s", empty)
				}
			}
		}
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{"procurement.requisitions.list", map[string]any{"query": "SECRET-42"}},
		{"procurement.requisitions.list", map[string]any{"offset": 1}},
		{"cores.search", map[string]any{"query": "SECRET-42"}},
		{"cores.query.records", map[string]any{"queries": []map[string]any{{"alias": "requests", "entity": "procurement.requisitions", "search": "SECRET-42"}}}},
	} {
		if strings.Contains(prettyJSON(call(member, tc.name, tc.args).Data), "SECRET-42") {
			t.Fatal("foreign search leaked")
		}
		if tc.name == "procurement.requisitions.list" && len(call(member, tc.name, tc.args).Data.([]any)) != 0 {
			t.Fatal("search/pagination applied before rights")
		}
	}
	for _, entity := range []string{"procurement.requisitions", "procurement.requisition_lines"} {
		rows := call(member, "cores.query.aggregate", map[string]any{"entity": entity}).Data.([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["record_count"] != float64(1) {
			t.Fatalf("foreign aggregate rows leaked: %v", rows)
		}
	}
	sum := call(member, "cores.query.aggregate", map[string]any{"entity": "procurement.requisition_lines", "metrics": []map[string]any{{"function": "sum", "field": "quantity", "alias": "quantity"}}}).Data.([]any)
	if sum[0].(map[string]any)["quantity"] != float64(1) {
		t.Fatalf("foreign quantity leaked: %v", sum)
	}
	overview := call(member, "cores.operations.overview", map[string]any{}).Data.(map[string]any)
	if overview["open_requisitions"] != float64(1) {
		t.Fatalf("foreign overview count leaked: %v", overview)
	}
	recommendation := call(member, "inventory.procurement.recommendations", map[string]any{"query": "Shared", "from": "2026-01-01", "to": "2026-12-31"}).Data.([]any)
	if len(recommendation) != 1 || recommendation[0].(map[string]any)["open_requisitions"] != float64(1) {
		t.Fatalf("foreign product demand count leaked: %v", recommendation)
	}
	if _, err := database.Exec("UPDATE users SET is_admin=false WHERE userid=43"); err != nil {
		t.Fatal(err)
	}
	if got := prettyJSON(call(admin, "cores.query.records", records).Data); strings.Contains(got, "VISIBLE-41") || strings.Contains(got, "SECRET-42") {
		t.Fatal("revoked admin retained access through old token")
	}
	if _, err := database.Exec("UPDATE users SET is_active=false WHERE userid=41"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		got := prettyJSON(call(member, tc.name, tc.args).Data)
		if strings.Contains(got, "VISIBLE-41") || strings.Contains(got, "SECRET-42") {
			t.Fatalf("inactive user retained access via %s", tc.name)
		}
	}
	// A direct anonymous database read following authenticated traffic cannot
	// inherit transaction-local identity, even on the same pooled connection.
	rows, err := repository.Query(context.Background(), "SELECT r.id FROM proc_requisitions r WHERE "+procurementRequisitionReadAccess)
	if err != nil || len(rows) != 0 {
		t.Fatalf("anonymous identity leaked: %v %v", rows, err)
	}
}

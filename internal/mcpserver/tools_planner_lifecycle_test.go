package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestPlannerLifecycleOwnerScopesRetryReplayAndRevocation(t *testing.T) {
	secret := strings.Repeat("planner-owner-", 4)
	var calls atomic.Int64
	var revoked atomic.Bool
	failed := map[string]bool{}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/planner/mcp/"), "/")
		if len(parts) != 2 {
			t.Error(r.URL.Path)
			w.WriteHeader(404)
			return
		}
		op := parts[1]
		action := op
		if op == "restore" {
			action = "archive"
		}
		cookie, e := r.Cookie("cores_token")
		if e != nil {
			t.Error(e)
			w.WriteHeader(403)
			return
		}
		claims := &suiteServiceClaims{}
		token, e := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if e != nil || !token.Valid || claims.UserID != 7 || claims.IsAdmin || claims.FinancialScope || claims.MutationScope != "cores:planner:"+action {
			t.Error("real member/selected action/no admin or financial delegation", e)
			w.WriteHeader(403)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("body")
		}
		if body["dry_run"] != nil || body["idempotency_key"] != nil {
			t.Error("transport fields leaked", body)
		}
		if body["preview"] == true {
			if body["confirm_change"] == true || body["confirm_creation"] == true || r.Header.Get("Idempotency-Key") != "" {
				t.Error("mutating preview", body)
			}
			io.WriteString(w, `{"preview":true,"ready_to_execute":true}`)
			return
		}
		if revoked.Load() {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":"membership revoked"}`)
			return
		}
		key := strings.Join(parts, ".")
		if !failed[key] {
			failed[key] = true
			w.WriteHeader(500)
			io.WriteString(w, `{"error":"synthetic audit rollback"}`)
			return
		}
		io.WriteString(w, `{"operation_status":"done","record":{"id":"test-task"}}`)
	}))
	defer owner.Close()
	cfg := config.Config{JWTSecret: secret, PlannerURL: owner.URL}
	for _, kind := range []string{"plans", "tasks"} {
		for _, op := range []string{"create", "update", "archive", "restore"} {
			t.Run(kind+"."+op, func(t *testing.T) {
				name := "planner." + kind + "." + op
				action := op
				if op == "restore" {
					action = "archive"
				}
				input := PlannerTaskInput{PlannerControl: PlannerControl{MutationControl: MutationControl{DryRun: true}, PlanID: "11111111-1111-4111-8111-111111111111", ExpectedUpdatedAt: "exact-version", ExpectedContext: strings.Repeat("a", 64), ConfirmChange: op != "create", ConfirmCreation: op == "create"}}
				fn := func(ctx context.Context, in PlannerTaskInput) (any, []Source, []string, error) {
					return invokePlannerLifecycle(ctx, cfg, kind, op, in, false)
				}
				ctx := inventoryTestContext(t, "7", false, "cores:read", "cores:planner:"+action)
				run := func(ctx context.Context, wantError bool) {
					t.Helper()
					r, out, e := executeMutationTool(ctx, name, "Planner", input, fn)
					if e != nil || (r != nil && r.IsError) != wantError {
						t.Fatal(r, out, e)
					}
				}
				run(ctx, false)
				input.DryRun = false
				input.IdempotencyKey = "planner-lifecycle-owner-" + kind + "-" + op
				run(ctx, true)
				run(ctx, false)
				run(ctx, false)
				revoked.Store(true)
				run(ctx, true)
				revoked.Store(false)
				before := calls.Load()
				for _, denied := range []context.Context{inventoryTestContext(t, "7", true, "cores:read", "cores:write"), inventoryTestContext(t, "7", true, "cores:rental:"+action), inventoryTestContext(t, "service:automation", true, "cores:planner:"+action)} {
					run(denied, true)
				}
				if before != calls.Load() {
					t.Fatal("denied token reached owner")
				}
				if !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:planner:"+action {
					t.Fatal("owner registry", name)
				}
			})
		}
	}
}
func TestPlannerLifecycleOAuthThroughHTTPAndSchema(t *testing.T) {
	cfg := config.Config{EnableWrites: true, PublicURL: "https://cores.example"}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "7", Expiration: time.Now().Add(time.Hour), Scopes: []string{"cores:read", "cores:write"}, Extra: map[string]any{"is_admin": true}}, nil
	}
	endpoint := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})))
	defer endpoint.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "planner-oauth", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: "legacy"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	for _, kind := range []string{"plans", "tasks"} {
		for _, op := range []string{"create", "update", "archive", "restore"} {
			for _, prefix := range []string{"", "prepare_"} {
				name := "planner." + kind + "." + prefix + op
				result, e := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
				action := op
				if op == "restore" {
					action = "archive"
				}
				if e != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), "cores:planner:"+action) || strings.Contains(prettyJSON(result.Meta), ":financial") {
					t.Fatal("challenge", name, result, e)
				}
				scopes := positionOAuthScopes(name)
				if len(scopes) != 2 || !containsString(scopes, "cores:planner:"+action) {
					t.Fatal("descriptor", name, scopes)
				}
			}
		}
	}
	for _, entity := range []string{"planner.plans", "planner.tasks"} {
		raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()[entity]))
		for _, field := range []string{"expected_context", "expected_plan_updated_at", "creation_id", "confirmation_text", "prepare_restore"} {
			if !strings.Contains(string(raw), field) {
				t.Fatal(entity, field)
			}
		}
	}
}
func TestPlannerLifecycleRetainedReadsAndPermissionRevocation(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_AUTH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated _test PostgreSQL required")
	}
	u, e := url.Parse(dsn)
	if e != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated _test database required")
	}
	q := u.Query()
	q.Set("search_path", "mcp_planner_lifecycle_test")
	u.RawQuery = q.Encode()
	db, e := sql.Open("pgx", u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(q string) {
		t.Helper()
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE SCHEMA mcp_planner_lifecycle_test`)
	defer db.Exec(`DROP SCHEMA mcp_planner_lifecycle_test CASCADE`)
	exec(plannerFixture)
	exec(`INSERT INTO planner_plans(id,name) VALUES('11111111-1111-4111-8111-111111111111','VISIBLE plan'),('22222222-2222-4222-8222-222222222222','SECRET plan');INSERT INTO planner_members VALUES('11111111-1111-4111-8111-111111111111','7','owner'),('22222222-2222-4222-8222-222222222222','8','owner');INSERT INTO planner_tasks(id,plan_id,title,rich_text_notes,archived_at) VALUES('33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111','VISIBLE archived task','Business notes',NOW()),('44444444-4444-4444-8444-444444444444','22222222-2222-4222-8222-222222222222','SECRET task','SECRET note',NULL);INSERT INTO planner_tasks(id,plan_id,title) VALUES('55555555-5555-4555-8555-555555555555','11111111-1111-4111-8111-111111111111','VISIBLE active task');INSERT INTO planner_mutation_events(plan_id,entity_type,entity_id,user_id,origin,operation,before_values,after_values) VALUES('11111111-1111-4111-8111-111111111111','task','33333333-3333-4333-8333-333333333333','7','MCP/AI','archive','{"rich_text_notes":"PRIVATE_AUDIT_NOTE"}','{"rich_text_notes":"PRIVATE_AUDIT_NOTE","updated_at":"exact-version","archived_at":"2026-10-16"}')`)
	ctx := inventoryTestContext(t, "7", true, "cores:read")
	server := New(config.Config{}, store.New(db, 5*time.Second, 200), slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, b := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "planner-read", Version: "1"}, nil).Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	call := func(name string, args map[string]any) string {
		t.Helper()
		r, e := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || r.IsError {
			t.Fatal(name, r, e)
		}
		out := prettyJSON(r.StructuredContent)
		if strings.Contains(out, "SECRET") || strings.Contains(out, "PRIVATE_") {
			t.Fatal("private leakage", name, out)
		}
		return out
	}
	for _, include := range []bool{false, true} {
		out := call("planner.tasks.search", map[string]any{"include_archived": include})
		if strings.Contains(out, "VISIBLE archived task") != include || !strings.Contains(out, "VISIBLE active task") {
			t.Fatal("archive discovery", include, out)
		}
	}
	if out := call("planner.tasks.get", map[string]any{"task_id": "33333333-3333-4333-8333-333333333333"}); !strings.Contains(out, "Business notes") || !strings.Contains(out, "plan_updated_at") {
		t.Fatal(out)
	}
	if out := call("planner.tasks.audit_history", map[string]any{"id": "33333333-3333-4333-8333-333333333333"}); !strings.Contains(out, "exact-version") || !strings.Contains(out, "MCP/AI") {
		t.Fatal(out)
	}
	if out := call("planner.plans.get", map[string]any{"id": "11111111-1111-4111-8111-111111111111"}); !strings.Contains(out, "Member Seven") || strings.Contains(out, "VISIBLE archived task") {
		t.Fatal(out)
	}
	exec(`UPDATE planner_plans SET archived_at=NOW() WHERE id='11111111-1111-4111-8111-111111111111'`)
	if out := call("planner.tasks.search", map[string]any{}); strings.Contains(out, "VISIBLE") {
		t.Fatal("archived plan workload", out)
	}
	if out := call("planner.plans.search", map[string]any{"include_archived": true}); !strings.Contains(out, "VISIBLE plan") {
		t.Fatal(out)
	}
	exec(`DELETE FROM planner_members WHERE user_id='7'`)
	for name, args := range map[string]map[string]any{"planner.plans.search": {"include_archived": true}, "planner.tasks.search": {"include_archived": true}, "planner.tasks.get": {"task_id": "33333333-3333-4333-8333-333333333333"}, "planner.tasks.audit_history": {"id": "33333333-3333-4333-8333-333333333333"}, "planner.plans.get": {"id": "11111111-1111-4111-8111-111111111111"}} {
		if out := call(name, args); strings.Contains(out, "VISIBLE") || strings.Contains(out, "exact-version") || strings.Contains(out, "MCP/AI") {
			t.Fatal("revocation", name, out)
		}
	}
}

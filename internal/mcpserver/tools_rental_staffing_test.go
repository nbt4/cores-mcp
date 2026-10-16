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

func TestRentalStaffingOwnerDelegationReplayRetryAndRevocation(t *testing.T) {
	var calls atomic.Int64
	var revoked atomic.Bool
	secret := strings.Repeat("staffing-owner-", 4)
	failed := map[string]bool{}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		op := strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/staffing/")
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
		if e != nil || !token.Valid || claims.UserID != 742 || !claims.IsAdmin || claims.FinancialScope || claims.MutationScope != "cores:rental:"+action || r.Header.Get("X-Cores-Origin") != "MCP/AI" {
			t.Error("real actor/action/no-financial delegation", e)
			w.WriteHeader(403)
			return
		}
		body := map[string]any{}
		if e = json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["job_id"] != float64(1) || body["employee_id"] != float64(2) || body["expected_job_updated_at"] != "job-version" || body["dry_run"] != nil || body["idempotency_key"] != nil {
			t.Error("identity/version/body", body)
		}
		if body["preview"] == true {
			if body["confirm_change"] == true || r.Header.Get("Idempotency-Key") != "" {
				t.Error("confirmed preparation", body)
			}
			io.WriteString(w, `{"preview":true,"ready_to_execute":true}`)
			return
		}
		if revoked.Load() {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":"owner current admin revoked"}`)
			return
		}
		if !failed[op] {
			failed[op] = true
			w.WriteHeader(500)
			io.WriteString(w, `{"error":"synthetic rollback"}`)
			return
		}
		io.WriteString(w, `{"operation_status":"done","assignment":{"job_id":1,"employee_id":2}}`)
	}))
	defer owner.Close()
	cfg := config.Config{JWTSecret: secret, RentalURL: owner.URL}
	for _, op := range []string{"create", "update", "archive", "restore"} {
		t.Run(op, func(t *testing.T) {
			action := op
			if op == "restore" {
				action = "archive"
			}
			name := "rental.staffing." + op
			input := RentalStaffingInput{MutationControl: MutationControl{DryRun: true}, JobID: 1, EmployeeID: 2, ExpectedJobUpdatedAt: "job-version", ConfirmChange: true}
			fn := func(ctx context.Context, in RentalStaffingInput) (any, []Source, []string, error) {
				return invokeRentalStaffing(ctx, cfg, op, in, false)
			}
			ctx := inventoryTestContext(t, "742", true, "cores:read", "cores:rental:"+action)
			run := func(ctx context.Context, wantError bool) {
				t.Helper()
				result, out, e := executeMutationTool(ctx, name, "Staffing", input, fn)
				if e != nil || (result != nil && result.IsError) != wantError {
					t.Fatal(result, out, e)
				}
			}
			run(ctx, false)
			input.DryRun = false
			input.IdempotencyKey = "staffing-owner-test-" + op
			run(ctx, true)
			run(ctx, false)
			run(ctx, false)
			revoked.Store(true)
			run(ctx, true)
			revoked.Store(false)
			before := calls.Load()
			for _, denied := range []context.Context{inventoryTestContext(t, "742", true, "cores:read"), inventoryTestContext(t, "742", false, "cores:rental:"+action), inventoryTestContext(t, "service:test", true, "cores:rental:"+action)} {
				run(denied, true)
			}
			if before != calls.Load() {
				t.Fatal("denied replay reached owner")
			}
			if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:"+action {
				t.Fatal("registry", name)
			}
		})
	}
}
func TestRentalStaffingOAuthChallengeAndDiscovery(t *testing.T) {
	cfg := config.Config{EnableWrites: true, PublicURL: "https://cores.example"}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Scopes: []string{"cores:read", "cores:write"}, Extra: map[string]any{"is_admin": true}}, nil
	}
	endpoint := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})))
	defer endpoint.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "staffing-http-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: "legacy"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	for _, op := range []string{"create", "update", "archive", "restore"} {
		for _, prefix := range []string{"", "prepare_"} {
			name := "rental.staffing." + prefix + op
			result, e := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"job_id": 1, "employee_id": 2}})
			action := op
			if op == "restore" {
				action = "archive"
			}
			if e != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), "cores:rental:"+action) || strings.Contains(prettyJSON(result.Meta), ":financial") {
				t.Fatal("scope challenge", name, result, e)
			}
			scopes := positionOAuthScopes(name)
			if len(scopes) != 2 || !containsString(scopes, "cores:rental:"+action) {
				t.Fatal(name, scopes)
			}
		}
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.staffing"]))
	for _, field := range []string{"employee_id", "role", "allow_overlap", "expected_job_updated_at", "expected_updated_at", "confirmation_text", "prepare_restore"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("discovery", field)
		}
	}
}
func TestRentalStaffingReadsArchivedPrivacyAndCurrentRights(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_RENTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated _test PostgreSQL required")
	}
	u, e := url.Parse(dsn)
	if e != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated test database required")
	}
	q := u.Query()
	q.Set("search_path", "mcp_staffing_lifecycle_test")
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
	exec(`CREATE SCHEMA mcp_staffing_lifecycle_test`)
	defer db.Exec(`DROP SCHEMA mcp_staffing_lifecycle_test CASCADE`)
	exec(`CREATE TABLE users(userid INT PRIMARY KEY,is_admin BOOL,is_active BOOL);INSERT INTO users VALUES(1,true,true);
 CREATE TABLE employees(id INT PRIMARY KEY,first_name TEXT,last_name TEXT,is_active BOOL,email TEXT,iban TEXT,street TEXT,notes TEXT);INSERT INTO employees VALUES(1,'Ada','Staffing',true,'PRIVATE_EMAIL','PRIVATE_BANK','PRIVATE_ADDRESS','PRIVATE_NOTE'),(2,'Ben','Archived',false,'PRIVATE_EMAIL',NULL,NULL,NULL);
 CREATE TABLE skills(id INT PRIMARY KEY,name TEXT,category TEXT);INSERT INTO skills VALUES(1,'Audio','Technik');CREATE TABLE employee_skills(employee_id INT,skill_id INT);INSERT INTO employee_skills VALUES(1,1);
 CREATE TABLE jobs(jobid INT PRIMARY KEY,startdate DATE,enddate DATE,updated_at TIMESTAMP);INSERT INTO jobs VALUES(1,'2026-10-01','2026-10-04',NOW());
 CREATE TABLE job_employees(job_id INT,employee_id INT,role TEXT,deleted_at TIMESTAMP,updated_at TIMESTAMP);INSERT INTO job_employees VALUES(1,1,'Ton',NULL,NOW()),(1,2,'Licht',NOW(),NOW());
 CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id INT,timestamp TIMESTAMP,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB);INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values) VALUES(1,'rental.staffing.archive','rental_staffing','1/2','{"role":"Licht","iban":"PRIVATE_BANK"}','{"origin":"MCP/AI","after":{"is_archived":true,"role":"Licht","updated_at":"exact-version"},"email":"PRIVATE_EMAIL"}');`)
	ctx := inventoryTestContext(t, "1", true, "cores:read")
	server := New(config.Config{}, store.New(db, 5*time.Second, 200), slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, b := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "staff-read", Version: "1"}, nil).Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	call := func(name string, args map[string]any) string {
		t.Helper()
		result, e := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil || result.IsError {
			t.Fatal(name, result, e)
		}
		data := prettyJSON(result.StructuredContent)
		if strings.Contains(data, "PRIVATE_") {
			t.Fatal("private fields leaked", data)
		}
		return data
	}
	for _, include := range []bool{false, true} {
		data := call("rental.staffing.search", map[string]any{"job_id": 1, "include_archived": include})
		if strings.Contains(data, `"employee_id": 2`) != include {
			t.Fatal("archived staffing", include, data)
		}
	}
	if data := call("rental.staffing.get", map[string]any{"job_id": 1, "employee_id": 2}); !strings.Contains(data, `"is_archived": true`) {
		t.Fatal(data)
	}
	if data := call("rental.employees.search", map[string]any{"query": "Audio"}); !strings.Contains(data, "Ada Staffing") || strings.Contains(data, "Ben Archived") {
		t.Fatal(data)
	}
	if data := call("rental.employees.search", map[string]any{"include_inactive": true}); !strings.Contains(data, "Ben Archived") {
		t.Fatal(data)
	}
	call("rental.skills.search", map[string]any{})
	if data := call("rental.staffing.audit_history", map[string]any{"job_id": 1, "employee_id": 2}); !strings.Contains(data, "exact-version") || !strings.Contains(data, "MCP/AI") {
		t.Fatal(data)
	}
	exec(`UPDATE users SET is_admin=false WHERE userid=1`)
	for name, args := range map[string]map[string]any{"rental.employees.search": {}, "rental.skills.search": {}, "rental.staffing.get": {"job_id": 1, "employee_id": 2}, "rental.staffing.search": {"job_id": 1, "include_archived": true}, "rental.staffing.audit_history": {"job_id": 1, "employee_id": 2}} {
		data := call(name, args)
		if strings.Contains(data, "Ada Staffing") || strings.Contains(data, "Ben Archived") || strings.Contains(data, "MCP/AI") || strings.Contains(data, "Audio") {
			t.Fatal("stale admin read", name, data)
		}
	}
}

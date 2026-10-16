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

func TestRentalAssignmentLifecycleOwnerRetryReplayAndScopeRevocation(t *testing.T) {
	var calls atomic.Int64
	var revoked atomic.Bool
	secret := strings.Repeat("catalog-lifecycle-", 3)
	failed := map[string]bool{}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		op := strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/jobs/external-equipment-")
		action := op
		if op == "restore" {
			action = "archive"
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Error(err)
			w.WriteHeader(403)
			return
		}
		claims := &suiteServiceClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.UserID != 742 || !claims.IsAdmin || !claims.FinancialScope || claims.MutationScope != "cores:rental:"+action {
			t.Error("owner identity/action/financial delegation", err)
			w.WriteHeader(403)
			return
		}
		body := map[string]any{}
		if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, key := range []string{"dry_run", "idempotency_key", "entity", "table"} {
			if body[key] != nil {
				t.Error("arbitrary or transport-only field reached owner", key)
			}
		}
		if body["job_id"] != float64(1) || body["position_id"] != float64(2) || body["equipment_id"] != float64(1) || body["expected_updated_at"] != "2026-10-15T10:20:30.000001Z" {
			t.Error("lost exact identity/version", body)
		}
		if body["preview"] == true {
			if r.Header.Get("Idempotency-Key") != "" || body["confirm_change"] == true {
				t.Error("preview mutated", body)
			}
			io.WriteString(w, `{"preview":true,"ready_to_execute":true,"draft":{"equipment_id":1}}`)
			return
		}
		if revoked.Load() {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":"current owner administrator revoked"}`)
			return
		}
		if !failed[op] {
			failed[op] = true
			w.WriteHeader(500)
			io.WriteString(w, `{"error":"synthetic audit rollback"}`)
			return
		}
		io.WriteString(w, `{"equipment":{"equipment_id":1,"rental_price":13.25},"operation_status":"done"}`)
	}))
	defer owner.Close()
	cfg := config.Config{JWTSecret: secret, RentalURL: owner.URL}
	for _, op := range []string{"update", "archive", "restore"} {
		t.Run(op, func(t *testing.T) {
			name := "rental.job_external_equipment." + op
			action := op
			if op == "restore" {
				action = "archive"
			}
			input := RentalAssignmentUpdateInput{RentalAssignmentControl: RentalAssignmentControl{MutationControl: MutationControl{DryRun: true}, JobID: 1, EquipmentID: 1, PositionID: 2, ExpectedUpdatedAt: "2026-10-15T10:20:30.000001Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "BOUND CATALOG DRAFT", ConfirmChange: true}}
			fn := func(ctx context.Context, in RentalAssignmentUpdateInput) (any, []Source, []string, error) {
				return invokeRentalAssignmentChange(ctx, cfg, op, in, false)
			}
			ctx := inventoryTestContext(t, "742", true, "cores:read", "cores:rental:"+action, rentalFinancialScope)
			run := func(ctx context.Context, wantError bool) {
				t.Helper()
				result, out, err := executeMutationTool(ctx, name, "Catalog", input, fn)
				if err != nil || (result != nil && result.IsError) != wantError {
					t.Fatal(result, out, err)
				}
			}
			run(ctx, false)
			input.DryRun = false
			input.IdempotencyKey = "assignment-lifecycle-test-" + op
			run(ctx, true)
			run(ctx, false)
			run(ctx, false)
			revoked.Store(true)
			run(ctx, true)
			revoked.Store(false)
			before := calls.Load()
			for _, denied := range []context.Context{inventoryTestContext(t, "742", true, "cores:write"), inventoryTestContext(t, "742", true, "cores:rental:"+action), inventoryTestContext(t, "742", true, "cores:rental:"+action, warehouseFinancialScope), inventoryTestContext(t, "742", false, "cores:rental:"+action, rentalFinancialScope), inventoryTestContext(t, "service:test", true, "cores:rental:"+action, rentalFinancialScope)} {
				run(denied, true)
			}
			if calls.Load() != before {
				t.Fatal("denied cached replay reached owner")
			}
			if !isMutationTool(name) || !hasDurableWarehouseRetry(name) || requiredMutationScope(name) != "cores:rental:"+action {
				t.Fatal("catalog guard registry incomplete", name)
			}
		})
	}
}

func TestRentalAssignmentLifecycleConsentThroughHTTP(t *testing.T) {
	cfg := config.Config{EnableWrites: true, PublicURL: "https://cores.example"}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Scopes: []string{"cores:read", "cores:write"}, Extra: map[string]any{"is_admin": true}}, nil
	}
	endpoint := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})))
	defer endpoint.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "catalog-consent", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: "legacy"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, op := range []string{"update", "archive", "restore"} {
		for _, operation := range []string{op, "prepare_" + op} {
			name := "rental.job_external_equipment." + operation
			result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"job_id": 1, "equipment_id": 1, "position_id": 2}})
			if err != nil || result == nil || !result.IsError || !strings.Contains(prettyJSON(result.Meta["mcp/www_authenticate"]), rentalFinancialScope) {
				t.Fatal("missing catalog consent challenge", name, result, err)
			}
			action := op
			if op == "restore" {
				action = "archive"
			}
			scopes := positionOAuthScopes(name)
			if !containsString(scopes, "cores:rental:"+action) || !containsString(scopes, rentalFinancialScope) {
				t.Fatal("wrong discovery grant", name, scopes)
			}
		}
	}
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.job_external_equipment"]))
	for _, field := range []string{"expected_updated_at", "expected_context", "confirmation_text", "prepare_restore", "position_id", "customer_price"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing writable catalog discovery", field)
		}
	}
}

func TestRentalAssignmentReadsDiscoverArchivesAndRecheckRights(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_RENTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated _test PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated test database required")
	}
	q := u.Query()
	q.Set("search_path", "mcp_assignment_lifecycle_test")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP SCHEMA IF EXISTS mcp_assignment_lifecycle_test CASCADE;CREATE SCHEMA mcp_assignment_lifecycle_test`)
	defer db.Exec(`DROP SCHEMA mcp_assignment_lifecycle_test CASCADE`)
	raw, err := os.ReadFile("testdata/external_catalog_lifecycle.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(raw))
	exec(`ALTER TABLE job_rental_equipment ADD COLUMN notes TEXT;ALTER TABLE job_rental_equipment ADD COLUMN created_at TIMESTAMP DEFAULT NOW();ALTER TABLE job_rental_equipment ADD COLUMN updated_at TIMESTAMP DEFAULT NOW();
 ALTER TABLE job_positions ADD COLUMN quantity NUMERIC DEFAULT 2;ALTER TABLE job_positions ADD COLUMN description TEXT DEFAULT 'Synthetic line';ALTER TABLE job_positions ADD COLUMN unit TEXT DEFAULT 'Stück';ALTER TABLE job_positions ADD COLUMN follow_day_factor NUMERIC DEFAULT 0;ALTER TABLE job_positions ADD COLUMN discount_percent NUMERIC DEFAULT 0;ALTER TABLE job_positions ADD COLUMN discount_amount NUMERIC DEFAULT 0;ALTER TABLE job_positions ADD COLUMN tax_rate NUMERIC DEFAULT 19;ALTER TABLE job_positions ADD COLUMN sort_order INTEGER DEFAULT 0;ALTER TABLE job_positions ADD COLUMN updated_at TIMESTAMP DEFAULT NOW();
 INSERT INTO job_positions(job_id,rental_equipment_id,unit_price) VALUES(1,1,20);INSERT INTO job_positions(job_id,rental_equipment_id,unit_price,deleted_at) VALUES(1,2,30,NOW());
 INSERT INTO job_rental_equipment(job_id,equipment_id,position_id,quantity,days_used,rental_unit_price,total_cost,notes) VALUES(1,1,1,2,3,12.35,74.10,'Private supplier note'),(1,2,2,2,3,15,90,'Private archived note');
 INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values) VALUES(1,'rental.job_external_equipment.archive','rental_job_external_equipment','1/2','{"assignment":{"notes":"Private before","total_cost":90}}','{"origin":"MCP/AI","after":{"position_id":2,"updated_at":"2026-10-16T10:00:00.000001Z","total_cost":90,"notes":"Private after"},"position":{"is_archived":true,"updated_at":"2026-10-16T10:00:00.000002Z","unit_price":30}}')`)
	repository := store.New(db, 5*time.Second, 200)
	for _, financial := range []bool{false, true} {
		scopes := []string{"cores:read"}
		if financial {
			scopes = append(scopes, rentalFinancialScope)
		}
		ctx := inventoryTestContext(t, "1", true, scopes...)
		server := New(config.Config{}, repository, slog.New(slog.NewTextHandler(io.Discard, nil)))
		a, b := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ss.Close()
		client, err := mcp.NewClient(&mcp.Implementation{Name: "assignment-read-test", Version: "1"}, nil).Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		get, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.job_external_equipment.get", Arguments: map[string]any{"job_id": 1, "equipment_id": 2}})
		if err != nil || get.IsError == financial {
			t.Fatal("financial read gate", financial, get, err)
		}
		if financial {
			data := prettyJSON(get.StructuredContent)
			if !strings.Contains(data, "position_updated_at") || !strings.Contains(data, "90") || strings.Contains(data, "Private") {
				t.Fatal("retained read", data)
			}
			for _, include := range []bool{false, true} {
				result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.job_external_equipment.search", Arguments: map[string]any{"job_id": 1, "include_archived": include}})
				if err != nil || result.IsError {
					t.Fatal(result, err)
				}
				data := prettyJSON(result.StructuredContent)
				if strings.Contains(data, `"equipment_id": 2`) != include {
					t.Fatal("archived discovery", include, data)
				}
			}
		}
		audit, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.job_external_equipment.audit_history", Arguments: map[string]any{"job_id": 1, "equipment_id": 2}})
		if err != nil || audit.IsError {
			t.Fatal(audit, err)
		}
		data := prettyJSON(audit.StructuredContent)
		if !strings.Contains(data, "MCP/AI") || strings.Contains(data, "Private") || strings.Contains(data, "total_cost") || strings.Contains(data, "unit_price") {
			t.Fatal("history projection", data)
		}
		exec(`UPDATE users SET is_admin=false WHERE userid=1`)
		revoked, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.job_external_equipment.audit_history", Arguments: map[string]any{"job_id": 1, "equipment_id": 2}})
		if err != nil || strings.Contains(prettyJSON(revoked.StructuredContent), "MCP/AI") {
			t.Fatal("stale admin read", revoked, err)
		}
		exec(`UPDATE users SET is_admin=true WHERE userid=1`)
	}
}

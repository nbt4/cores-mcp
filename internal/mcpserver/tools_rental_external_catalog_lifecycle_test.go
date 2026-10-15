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

func TestExternalCatalogLifecycleOwnerRetryReplayAndScopeRevocation(t *testing.T) {
	var calls atomic.Int64
	var revoked atomic.Bool
	secret := strings.Repeat("catalog-lifecycle-", 3)
	failed := map[string]bool{}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		op := strings.TrimPrefix(r.URL.Path, "/api/v1/mcp/external-equipment/")
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
		for _, key := range []string{"dry_run", "idempotency_key", "job_id", "entity", "table"} {
			if body[key] != nil {
				t.Error("arbitrary or transport-only field reached owner", key)
			}
		}
		if body["equipment_id"] != float64(1) || body["expected_updated_at"] != "2026-10-15T10:20:30.000001Z" {
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
			name := "rental.external_equipment." + op
			action := op
			if op == "restore" {
				action = "archive"
			}
			input := RentalExternalCatalogUpdateInput{RentalExternalCatalogControl: RentalExternalCatalogControl{MutationControl: MutationControl{DryRun: true}, EquipmentID: 1, ExpectedUpdatedAt: "2026-10-15T10:20:30.000001Z", ExpectedContext: strings.Repeat("a", 64), ConfirmationText: "BOUND CATALOG DRAFT", ConfirmChange: true}}
			fn := func(ctx context.Context, in RentalExternalCatalogUpdateInput) (any, []Source, []string, error) {
				return invokeRentalExternalCatalogChange(ctx, cfg, op, in, false)
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
			input.IdempotencyKey = "external-lifecycle-test-" + op
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

func TestExternalCatalogLifecycleConsentThroughHTTP(t *testing.T) {
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
			name := "rental.external_equipment." + operation
			result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"equipment_id": 1}})
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
	raw, _ := json.Marshal(renderWritableEntitySchema(writableEntitySchemas()["rental.external_equipment"]))
	for _, field := range []string{"expected_updated_at", "expected_context", "confirmation_text", "prepare_restore", "supplier_id", "customer_price"} {
		if !strings.Contains(string(raw), field) {
			t.Fatal("missing writable catalog discovery", field)
		}
	}
}

func TestExternalCatalogReadsProjectMoneyAndRecheckDatabaseRights(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_RENTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated _test PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated test database required")
	}
	q := u.Query()
	q.Set("search_path", "mcp_external_catalog_lifecycle_test")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DROP SCHEMA IF EXISTS mcp_external_catalog_lifecycle_test CASCADE;CREATE SCHEMA mcp_external_catalog_lifecycle_test`)
	defer db.Exec(`DROP SCHEMA mcp_external_catalog_lifecycle_test CASCADE`)
	fixture, err := os.ReadFile("testdata/external_catalog_lifecycle.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(fixture))
	exec(`UPDATE rental_equipment SET is_active=false WHERE id=1;INSERT INTO rental_equipment(name,supplier,category,rental_price,customer_price) VALUES('Active second rental','Supplier','Audio',3,4);
 INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values) VALUES(1,'rental.external_equipment.update','rental_external_equipment','1','{"rental_price":12.35,"notes":"private old note"}','{"origin":"MCP/AI","after":{"name":"Synthetic Rental","is_active":false,"updated_at":"2026-10-15T10:00:00.000001Z","rental_price":13.25,"customer_price":25.50,"notes":"private new note"}}')`)
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
		client, err := mcp.NewClient(&mcp.Implementation{Name: "catalog-read-test", Version: "1"}, nil).Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		for _, tool := range []string{"get", "audit_history"} {
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.external_equipment." + tool, Arguments: map[string]any{"id": "1"}})
			if err != nil || result.IsError {
				t.Fatal(result, err)
			}
			data := prettyJSON(result.StructuredContent)
			if strings.Contains(data, "private") || strings.Contains(data, `"notes"`) {
				t.Fatal("private audit/catalog fields exposed", data)
			}
			if strings.Contains(data, `"rental_price`) != financial {
				t.Fatal("financial projection incorrect", financial, tool, data)
			}
		}
		for _, include := range []bool{false, true} {
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.external_equipment.search", Arguments: map[string]any{"include_inactive": include}})
			if err != nil || result.IsError {
				t.Fatal(result, err)
			}
			data := prettyJSON(result.StructuredContent)
			if strings.Contains(data, "Synthetic Rental") != include || !strings.Contains(data, "Active second rental") {
				t.Fatal("archive discovery filter", include, data)
			}
		}
		exec(`UPDATE users SET is_admin=false WHERE userid=1`)
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "rental.external_equipment.get", Arguments: map[string]any{"id": "1"}})
		if err != nil || strings.Contains(prettyJSON(result.StructuredContent), "Synthetic Rental") {
			t.Fatal("stale admin claims exposed catalog", result, err)
		}
		exec(`UPDATE users SET is_admin=true WHERE userid=1`)
	}
}

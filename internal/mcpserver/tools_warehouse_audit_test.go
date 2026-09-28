package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestWarehouseAuditHistoryIsAdminOnlyAndRedacted(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_WAREHOUSE_TEST_DATABASE_URL to a disposable PostgreSQL database ending in _test")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("integration test requires a dedicated _test database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_warehouse_audit_test"
	if _, err := database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	if _, err := database.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,ip_address TEXT,user_agent TEXT,timestamp TIMESTAMPTZ DEFAULT now())`,
		`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,ip_address) VALUES(42,'product.update','product','7','{"name":"Old mixer","lifecycle_status":"active","private_note":"hidden"}','{"origin":"MCP/AI","after":{"name":"New mixer","lifecycle_status":"active","private_note":"hidden"}}','192.0.2.1')`,
		`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,ip_address) VALUES(42,'product.archive','product','7','{"lifecycle_status":"active"}','{"lifecycle_status":"archived","archived_devices":2,"private_note":"hidden"}','192.0.2.1')`,
		`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,ip_address) VALUES(42,'product.update','procurement_product','7','{"name":"Foreign"}','{"name":"Foreign"}','192.0.2.1')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool) (any, error) {
		var result any
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "42", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			result, _, _, resultErr = warehouseAuditHistory(r.Context(), db, WarehouseAuditInput{ProductID: 7})
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return result, resultErr
	}
	if _, err := run(false); err == nil {
		t.Fatal("non-admin read Warehouse product audit")
	}
	result, err := run(true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	content := string(encoded)
	for _, want := range []string{"product.update", "product.archive", "Old mixer", "New mixer", "archived_devices"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %s: %s", want, content)
		}
	}
	for _, secret := range []string{"private_note", "192.0.2.1", "Foreign"} {
		if strings.Contains(content, secret) {
			t.Fatalf("leaked %s: %s", secret, content)
		}
	}
}

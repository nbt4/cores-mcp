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

func TestProcurementAuditHistoryIsScopedAndRedacted(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_PROCUREMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_PROCUREMENT_TEST_DATABASE_URL to a disposable PostgreSQL database ending in _test")
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
	const schema = "mcp_procurement_audit_test"
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
		`CREATE TABLE proc_requisitions(id BIGINT PRIMARY KEY,requester_id BIGINT)`,
		`CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,ip_address TEXT,user_agent TEXT,timestamp TIMESTAMPTZ DEFAULT now())`,
		`INSERT INTO proc_requisitions VALUES(7,41)`,
		`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,ip_address) VALUES(42,'requisition.approved','procurement_requisition','7','{"status":"submitted","private_note":"hidden"}','{"origin":"MCP/AI","requisition":{"status":"approved","estimatedTotalCents":2500,"private_note":"hidden"}}','192.0.2.1')`,
		`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,ip_address) VALUES(42,'order.goods_received','procurement_order','9','{"order":{"status":"sent"}}','{"origin":"MCP/AI","order":{"status":"partially_received"},"receipt":{"quantity":1,"private_note":"hidden"}}','192.0.2.1')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(user string, admin bool, input ProcurementAuditInput) (any, error) {
		var result any
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: user, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			result, _, _, resultErr = procurementAuditHistory(r.Context(), db, input)
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return result, resultErr
	}
	if _, err := run("43", false, ProcurementAuditInput{Entity: "requisition", ID: 7}); err == nil {
		t.Fatal("another requester read requisition audit")
	}
	if _, err := run("41", false, ProcurementAuditInput{Entity: "purchase_order", ID: 9}); err == nil {
		t.Fatal("requester read order audit")
	}
	for _, tc := range []struct {
		user   string
		admin  bool
		entity string
		id     int64
		action string
	}{{"41", false, "requisition", 7, "requisition.approved"}, {"42", true, "purchase_order", 9, "order.goods_received"}} {
		result, err := run(tc.user, tc.admin, ProcurementAuditInput{Entity: tc.entity, ID: tc.id})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), tc.action) || strings.Contains(string(encoded), "private_note") || strings.Contains(string(encoded), "192.0.2.1") {
			t.Fatalf("unsafe or missing audit data: %s", encoded)
		}
	}
}

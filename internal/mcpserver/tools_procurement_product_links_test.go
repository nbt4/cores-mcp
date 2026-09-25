package mcpserver

import (
	"context"
	"database/sql"
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

func TestPrepareProductLinkOwnershipVersionAndDependencies(t *testing.T) {
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
	const schema = "mcp_product_link_test"
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
		`CREATE TABLE proc_products(id BIGINT PRIMARY KEY,sku TEXT,name TEXT,active BOOLEAN,updated_at TIMESTAMPTZ)`,
		`CREATE TABLE products(productID BIGINT PRIMARY KEY,product_code TEXT,name TEXT,tracking_mode TEXT,lifecycle_status TEXT)`,
		`CREATE TABLE core_product_links(id BIGINT PRIMARY KEY,procurement_product_id BIGINT,warehouse_product_id BIGINT,updated_at TIMESTAMPTZ)`,
		`CREATE TABLE proc_receipts(id BIGINT PRIMARY KEY,purchase_order_line_id BIGINT)`,
		`CREATE TABLE proc_purchase_orders(id BIGINT PRIMARY KEY,number TEXT,status TEXT)`,
		`CREATE TABLE proc_purchase_order_lines(id BIGINT PRIMARY KEY,purchase_order_id BIGINT,product_id BIGINT)`,
		`INSERT INTO proc_products VALUES(1,'NODE-1','DMX Node',true,'2026-09-24T08:15:00.123456Z')`,
		`INSERT INTO products VALUES(11,'NODE-1','DMX Node','individual','active'),(12,'AMP-1','Amplifier','individual','active'),(13,'USED','Used','quantity','active')`,
		`INSERT INTO core_product_links VALUES(9,2,13,'2026-09-24T08:15:00Z')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool, input ProductLinkInput) (preparedMutation, error) {
		var p preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "41", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p, prepareErr = prepareProcurementProductLink(r.Context(), db, input)
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, prepareErr
	}
	input := ProductLinkInput{ProcurementProductID: 1, WarehouseProductID: 11}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin link preview accepted")
	}
	p, err := run(true, input)
	if err != nil || !p.Ready || p.Draft["expected_updated_at"] != "2026-09-24T08:15:00.123456Z" || p.Draft["confirmation_text_required"] != "LINK PROCUREMENT 1 WAREHOUSE 11" {
		t.Fatalf("new link preview: %#v %v", p, err)
	}
	input.ConfirmLink = true
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", p, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	p, err = run(true, input)
	if err != nil || !p.Ready {
		t.Fatalf("matching version rejected: %#v %v", p, err)
	}
	input.WarehouseProductID = 13
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "warehouse_product_id") {
		t.Fatalf("already linked target accepted: %#v %v", p, err)
	}
	if _, err := database.Exec(`INSERT INTO core_product_links VALUES(10,1,11,'2026-09-24T09:15:00.123456Z')`); err != nil {
		t.Fatal(err)
	}
	input.WarehouseProductID = 12
	input.AllowNameMismatch = true
	input.ExpectedUpdatedAt = "2026-09-24T09:15:00.123456Z"
	p, err = run(true, input)
	if err != nil || !p.Ready || len(p.Diff) != 1 {
		t.Fatalf("relink preview: %#v %v", p, err)
	}
	if _, err := database.Exec(`INSERT INTO proc_purchase_orders VALUES(5,'PO-5','sent')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO proc_purchase_order_lines VALUES(6,5,1)`); err != nil {
		t.Fatal(err)
	}
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "active_references") {
		t.Fatalf("open order did not block relink: %#v %v", p, err)
	}
}

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

func TestPrepareProcurementProductUpdateDiffVersionAndArchive(t *testing.T) {
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
	const schema = "mcp_procurement_product_update_test"
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
		`CREATE TABLE proc_products (id BIGSERIAL PRIMARY KEY,sku TEXT,name TEXT,description TEXT,category_id BIGINT,unit TEXT,manufacturer TEXT,model TEXT,parameters JSONB,attributes JSONB,active BOOLEAN,reorder_point DOUBLE PRECISION,target_stock DOUBLE PRECISION,updated_at TIMESTAMP)`,
		`CREATE TABLE proc_categories (id BIGINT PRIMARY KEY,name TEXT,parameter_schema JSONB)`,
		`CREATE TABLE proc_purchase_orders (id BIGINT PRIMARY KEY,number TEXT,status TEXT)`,
		`CREATE TABLE proc_purchase_order_lines (id BIGSERIAL PRIMARY KEY,purchase_order_id BIGINT,product_id BIGINT)`,
		`CREATE TABLE proc_requisitions (id BIGINT PRIMARY KEY,number TEXT,status TEXT)`,
		`CREATE TABLE proc_requisition_lines (id BIGSERIAL PRIMARY KEY,requisition_id BIGINT,product_id BIGINT)`,
		`INSERT INTO proc_categories VALUES (1,'Lighting','[]')`,
		`INSERT INTO proc_products(sku,name,description,category_id,unit,manufacturer,model,parameters,attributes,active,reorder_point,target_stock,updated_at) VALUES ('NODE-1','Node','DMX node',1,'Stk.','MA Lighting','grandMA3','{}','{}',true,1,2,'2026-09-24T08:15:00.123456'),('NODE-2','Other Node','Backup',1,'Stk.','','','{}','{}',true,0,0,'2026-09-24T08:15:00')`,
		`INSERT INTO proc_purchase_orders VALUES (7,'PO-7','ordered')`,
		`INSERT INTO proc_purchase_order_lines(purchase_order_id,product_id) VALUES (7,1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	lookup := store.New(database, 5*time.Second, 200)
	run := func(admin bool, input ProductUpdateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareProcurementProductUpdate(request.Context(), lookup, input)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	name := "Node Pro"
	input := ProductUpdateInput{ProductID: 1, Name: &name}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin product detail read accepted")
	}
	prepared, err := run(true, input)
	if err != nil || !prepared.Ready || len(prepared.Diff) != 1 || prepared.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" {
		t.Fatalf("update preview: %#v %v", prepared, err)
	}
	input.ConfirmUpdate = true
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", prepared, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready {
		t.Fatalf("matching version rejected: %#v %v", prepared, err)
	}
	duplicate := "NODE-2"
	input.SKU = &duplicate
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "duplicate_sku") {
		t.Fatalf("duplicate SKU accepted: %#v %v", prepared, err)
	}
	active := false
	input = ProductUpdateInput{ProductID: 1, Active: &active}
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "active_references") || len(prepared.RelatedRecords) == 0 {
		t.Fatalf("active order did not block archive: %#v %v", prepared, err)
	}
}

func TestProductJSONMapHandlesStoredJSON(t *testing.T) {
	value := productJSONMap(map[string]any{"ports": float64(4)})
	if value["ports"] != float64(4) {
		t.Fatalf("stored JSON lost: %#v", value)
	}
}

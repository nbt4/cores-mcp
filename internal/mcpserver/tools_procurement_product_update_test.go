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
		`CREATE TABLE proc_suppliers (id BIGINT PRIMARY KEY,name TEXT,code TEXT,active BOOLEAN)`,
		`CREATE TABLE proc_offers (id BIGSERIAL PRIMARY KEY,product_id BIGINT,supplier_id BIGINT,supplier_sku TEXT,price_cents BIGINT,currency TEXT,minimum_quantity DOUBLE PRECISION,pack_size DOUBLE PRECISION,lead_days BIGINT,purchase_url TEXT,valid_until TIMESTAMP,active BOOLEAN,updated_at TIMESTAMP)`,
		`CREATE TABLE proc_purchase_orders (id BIGINT PRIMARY KEY,number TEXT,status TEXT)`,
		`CREATE TABLE proc_purchase_order_lines (id BIGSERIAL PRIMARY KEY,purchase_order_id BIGINT,product_id BIGINT)`,
		`CREATE TABLE proc_requisitions (id BIGINT PRIMARY KEY,number TEXT,status TEXT)`,
		`CREATE TABLE proc_requisition_lines (id BIGSERIAL PRIMARY KEY,requisition_id BIGINT,product_id BIGINT)`,
		`INSERT INTO proc_categories VALUES (1,'Lighting','[]')`,
		`INSERT INTO proc_suppliers VALUES (1,'Light Supply','LS',true)`,
		`INSERT INTO proc_products(sku,name,description,category_id,unit,manufacturer,model,parameters,attributes,active,reorder_point,target_stock,updated_at) VALUES ('NODE-1','Node','DMX node',1,'Stk.','MA Lighting','grandMA3','{}','{}',true,1,2,'2026-09-24T08:15:00.123456'),('NODE-2','Other Node','Backup',1,'Stk.','','','{}','{}',true,0,0,'2026-09-24T08:15:00')`,
		`INSERT INTO proc_purchase_orders VALUES (7,'PO-7','ordered')`,
		`INSERT INTO proc_purchase_order_lines(purchase_order_id,product_id) VALUES (7,1)`,
		`INSERT INTO proc_offers(product_id,supplier_id,supplier_sku,price_cents,currency,minimum_quantity,pack_size,lead_days,purchase_url,active,updated_at) VALUES (1,1,'NODE-1',12000,'EUR',1,1,4,'',true,'2026-09-24T08:15:00.123456')`,
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
	runOfferCreate := func(input OfferCreateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareOfferCreate(request.Context(), lookup, input)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	price := int64(13000)
	offerCreate := OfferCreateInput{ProductID: 1, SupplierID: 1, PriceCents: &price}
	prepared, err = runOfferCreate(offerCreate)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "duplicate_offer") {
		t.Fatalf("duplicate offer accepted: %#v %v", prepared, err)
	}
	offerCreate.AllowDuplicate = true
	prepared, err = runOfferCreate(offerCreate)
	if err != nil || !prepared.Ready {
		t.Fatalf("confirmed duplicate offer blocked: %#v %v", prepared, err)
	}
	runOfferUpdate := func(input OfferUpdateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareOfferUpdate(request.Context(), lookup, input)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	offerUpdate := OfferUpdateInput{OfferID: 1, PriceCents: &price}
	prepared, err = runOfferUpdate(offerUpdate)
	if err != nil || !prepared.Ready || len(prepared.Diff) != 1 || prepared.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" {
		t.Fatalf("offer update preview: %#v %v", prepared, err)
	}
	offerUpdate.ConfirmUpdate = true
	prepared, err = runOfferUpdate(offerUpdate)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "expected_updated_at") {
		t.Fatalf("offer missing version accepted: %#v %v", prepared, err)
	}
	offerUpdate.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	prepared, err = runOfferUpdate(offerUpdate)
	if err != nil || !prepared.Ready {
		t.Fatalf("offer matching version rejected: %#v %v", prepared, err)
	}
}

func TestProductJSONMapHandlesStoredJSON(t *testing.T) {
	value := productJSONMap(map[string]any{"ports": float64(4)})
	if value["ports"] != float64(4) {
		t.Fatalf("stored JSON lost: %#v", value)
	}
}

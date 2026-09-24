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

func TestPrepareWarehouseProductUpdateDiffVersionAndDuplicates(t *testing.T) {
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
	const schema = "mcp_warehouse_product_update_test"
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
		`CREATE TABLE products (
			productid SERIAL PRIMARY KEY, name TEXT NOT NULL, categoryid INT, subcategoryid TEXT, subbiercategoryid TEXT,
			manufacturerid INT, brandid INT, description TEXT, maintenanceinterval INT, itemcostperday FLOAT8,
			weight FLOAT8, height FLOAT8, width FLOAT8, depth FLOAT8, powerconsumption FLOAT8, pos_in_category INT,
			count_type_id INT, stock_quantity FLOAT8, min_stock_level FLOAT8, generic_barcode TEXT, price_per_unit FLOAT8,
			product_type TEXT, tracking_mode TEXT, lifecycle_status TEXT, product_code TEXT, product_kind TEXT,
			model_number TEXT, manufacturer_part_number TEXT, ean TEXT, attributes JSONB, updated_at TIMESTAMP)`,
		`CREATE TABLE devices (deviceid TEXT PRIMARY KEY, productid INT)`,
		`CREATE TABLE product_locations (product_id INT, quantity FLOAT8)`,
		`INSERT INTO products(name,description,manufacturerid,generic_barcode,product_type,tracking_mode,lifecycle_status,product_code,product_kind,attributes,updated_at) VALUES
			('Stage Mixer','Four-channel mixer',7,'MIX-001','equipment','individual','active','PRD-000001','standard','{"ports":4}','2026-09-24T08:15:00.123456'),
			('Stage Mixer Pro','Another product',7,'MIX-002','equipment','individual','active','PRD-000002','standard','{}','2026-09-24T08:15:00')`,
		`INSERT INTO devices(deviceid,productid) VALUES ('DEV-001',1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	lookup := store.New(database, 5*time.Second, 200)
	run := func(admin bool, input WarehouseProductUpdateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareWarehouseProductUpdate(request.Context(), lookup, input)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	name := "Stage Mixer Deluxe"
	input := WarehouseProductUpdateInput{ProductID: 1, Name: &name}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin product detail read accepted")
	}
	prepared, err := run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "similar_product_review") || len(prepared.Diff) != 1 {
		t.Fatalf("similar product preview: %#v, %v", prepared, err)
	}
	input.AllowSimilarProduct = true
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready || prepared.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" || prepared.Draft["generic_barcode"] != "MIX-001" {
		t.Fatalf("complete product draft: %#v, %v", prepared, err)
	}
	input.ConfirmUpdate = true
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v, %v", prepared, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00Z"
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "expected_updated_at") {
		t.Fatalf("stale version accepted: %#v, %v", prepared, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready {
		t.Fatalf("matching version rejected: %#v, %v", prepared, err)
	}
	duplicate := "MIX-002"
	input.GenericBarcode = &duplicate
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "duplicate_product") {
		t.Fatalf("duplicate barcode accepted: %#v, %v", prepared, err)
	}
	quantity := "quantity"
	input = WarehouseProductUpdateInput{ProductID: 1, TrackingMode: &quantity}
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "tracking_mode") {
		t.Fatalf("device tracking guard missed: %#v, %v", prepared, err)
	}
}

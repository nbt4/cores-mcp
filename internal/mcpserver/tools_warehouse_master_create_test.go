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

func TestWarehouseStandaloneMasterPreparations(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_WAREHOUSE_TEST_DATABASE_URL to a disposable _test database")
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
	const schema = "mcp_warehouse_master_create_test"
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
		`CREATE TABLE manufacturer(manufacturerid SERIAL PRIMARY KEY,name TEXT,website TEXT)`,
		`CREATE TABLE brands(brandid SERIAL PRIMARY KEY,name TEXT,manufacturerid INT)`,
		`CREATE TABLE categories(categoryid SERIAL PRIMARY KEY,name TEXT,abbreviation TEXT)`,
		`CREATE TABLE subcategories(subcategoryid TEXT PRIMARY KEY,name TEXT,abbreviation TEXT,categoryid INT)`,
		`CREATE TABLE subbiercategories(subbiercategoryid TEXT PRIMARY KEY,name TEXT,abbreviation TEXT,subcategoryid TEXT)`,
		`CREATE TABLE storage_zones(zone_id SERIAL PRIMARY KEY,code TEXT,barcode TEXT,name TEXT,location TEXT,process_role TEXT,operational_status TEXT,is_active BOOLEAN,parent_zone_id INT)`,
		`INSERT INTO manufacturer(name,website) VALUES('MA Lighting','https://www.malighting.com'),('Robe Lighting',NULL)`,
		`INSERT INTO brands(name,manufacturerid) VALUES('grandMA3',1)`,
		`INSERT INTO categories(name,abbreviation) VALUES('Lighting','LT')`,
		`INSERT INTO subcategories VALUES('sub-1','Control','CTRL',1)`,
		`INSERT INTO subbiercategories VALUES('third-1','Network','NET','sub-1')`,
		`INSERT INTO storage_zones(code,barcode,name,location,process_role,operational_status,is_active) VALUES('MAIN','LOC-MAIN','Main Warehouse','Berlin','storage','available',true)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(fn func(context.Context) (preparedMutation, error)) (preparedMutation, error) {
		var p preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p, resultErr = fn(r.Context())
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, resultErr
	}
	manufacturer := WarehouseManufacturerCreateInput{Name: "ma lighting", Website: "www.malighting.com"}
	p, err := run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseManufacturerCreate(ctx, db, manufacturer)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_manufacturer") || *p.Draft["website"].(*string) != "https://www.malighting.com" {
		t.Fatalf("manufacturer duplicate: %#v %v", p, err)
	}
	manufacturer.Name = "New Manufacturer"
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseManufacturerCreate(ctx, db, manufacturer)
	})
	if err != nil || !p.Ready {
		t.Fatalf("standalone manufacturer draft: %#v %v", p, err)
	}
	brand := WarehouseBrandCreateInput{Name: "grandMA3", ManufacturerID: 1}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseBrandCreate(ctx, db, brand)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_brand") {
		t.Fatalf("brand duplicate: %#v %v", p, err)
	}
	brand.Name = "New Product Line"
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseBrandCreate(ctx, db, brand)
	})
	if err != nil || !p.Ready || p.Draft["manufacturer_id"] != int64(1) {
		t.Fatalf("standalone brand draft: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "category", "lighting", "LT", nil, false)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("category duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "subcategory", "Control", "", int64(1), false)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("subcategory duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "third_category", "Data", "", "sub-1", false)
	})
	if err != nil || !p.Ready || p.Draft["subcategory_id"] != "sub-1" {
		t.Fatalf("third category draft: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseLocationCreate(ctx, db, WarehouseLocationCreateInput{Code: "main", Name: "Main Warehouse"})
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_location") {
		t.Fatalf("location duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseLocationCreate(ctx, db, WarehouseLocationCreateInput{Code: "SHELF-A", Name: "Shelf A", ParentZoneID: 1})
	})
	if err != nil || !p.Ready || p.Draft["parent_zone_id"] != int64(1) {
		t.Fatalf("location draft: %#v %v", p, err)
	}
}

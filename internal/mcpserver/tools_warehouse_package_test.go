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

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestWarehousePackageSchemaIncludesContents(t *testing.T) {
	schema := renderWritableEntitySchema(writableEntitySchemas()["warehouse.packages"])
	fields, ok := schema["item_fields"].([]map[string]any)
	if !ok || len(fields) != 3 || fields[0]["name"] != "product_id" || fields[0]["required"] != true || fields[1]["name"] != "quantity" || fields[1]["required"] != true {
		t.Fatalf("incomplete item schema: %#v", schema)
	}
}

func TestWarehousePackagePreparation(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_WAREHOUSE_TEST_DATABASE_URL to a disposable _test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("dedicated _test database required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_warehouse_package_test"
	if _, err = database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE; CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	exec := func(query string) {
		t.Helper()
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`CREATE TABLE products(productid INT PRIMARY KEY,name TEXT,lifecycle_status TEXT)`,
		`INSERT INTO products VALUES(1,'Alpha','active'),(2,'Beta','active'),(3,'Retired','archived')`,
		`CREATE TABLE product_packages(id INT PRIMARY KEY,name TEXT,code TEXT,package_code TEXT,description TEXT,price NUMERIC(10,2),category TEXT,website_visible BOOLEAN,alias_json TEXT,is_active BOOLEAN,updated_at TIMESTAMP)`,
		`CREATE TABLE product_package_items(id SERIAL PRIMARY KEY,package_id INT,product_id INT,quantity INT,is_optional BOOLEAN)`,
		`CREATE TABLE job_packages(package_id INT,job_id INT)`,
		`INSERT INTO product_packages VALUES(1,'Sound Package','PKG-A','PKG-A','Details',12.34,'Sound',false,'["PA"]',true,'2026-09-30 10:15:00.123456'),(2,'Archived Package','PKG-B','PKG-B',NULL,NULL,NULL,false,NULL,false,'2026-09-30 10:15:00.123456')`,
		`INSERT INTO product_package_items(package_id,product_id,quantity,is_optional) VALUES(1,1,2,false),(1,2,3,true)`,
	} {
		exec(statement)
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool, fn func(context.Context) (preparedMutation, error)) (preparedMutation, error) {
		var p preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { p, resultErr = fn(r.Context()) }))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, resultErr
	}
	create := func(in WarehousePackageCreateInput) preparedMutation {
		t.Helper()
		p, err := run(true, func(ctx context.Context) (preparedMutation, error) { return prepareWarehousePackageCreate(ctx, db, in) })
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	update := func(in WarehousePackageUpdateInput) preparedMutation {
		t.Helper()
		p, err := run(true, func(ctx context.Context) (preparedMutation, error) { return prepareWarehousePackageUpdate(ctx, db, in) })
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	input := WarehousePackageCreateInput{Name: "New Fixture", Items: []WarehousePackageItem{{ProductID: 2, Quantity: 3, IsOptional: true}, {ProductID: 1, Quantity: 2}}, Aliases: []string{" Set ", "set", ""}}
	p := create(input)
	if !p.Ready || len(p.Draft["aliases"].([]string)) != 1 || p.Draft["items"].([]WarehousePackageItem)[0].ProductID != 1 {
		t.Fatalf("draft: %#v", p)
	}
	for _, tc := range []struct {
		name   string
		change func(*WarehousePackageCreateInput)
		field  string
	}{
		{"empty", func(i *WarehousePackageCreateInput) { i.Items = nil }, "items"},
		{"duplicate", func(i *WarehousePackageCreateInput) {
			i.Items = []WarehousePackageItem{{ProductID: 1, Quantity: 1}, {ProductID: 1, Quantity: 2}}
		}, "items"},
		{"archived", func(i *WarehousePackageCreateInput) { i.Items = []WarehousePackageItem{{ProductID: 3, Quantity: 1}} }, "items"},
		{"missing", func(i *WarehousePackageCreateInput) { i.Items = []WarehousePackageItem{{ProductID: 999, Quantity: 1}} }, "items"},
		{"zero", func(i *WarehousePackageCreateInput) { i.Items = []WarehousePackageItem{{ProductID: 1, Quantity: 0}} }, "items"},
		{"precision", func(i *WarehousePackageCreateInput) { v := 1.234; i.Price = &v }, "price"},
		{"name", func(i *WarehousePackageCreateInput) { i.Name = "Archived Package" }, "duplicate_package"},
		{"similar", func(i *WarehousePackageCreateInput) { i.Name = "Sound Package XL" }, "similar_package_review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			tc.change(&in)
			p := create(in)
			if p.Ready || !containsString(p.Missing, tc.field) {
				t.Fatalf("invalid draft: %#v", p)
			}
		})
	}
	allowed := input
	allowed.Name = "Sound Package XL"
	allowed.AllowSimilar = true
	if !create(allowed).Ready {
		t.Fatal("reviewed similar name should be allowed")
	}
	if _, err := run(false, func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehousePackageCreate(ctx, db, input)
	}); err == nil {
		t.Fatal("non-admin preview must fail")
	}
	name := "Updated Sound Fixture"
	in := WarehousePackageUpdateInput{PackageID: 1, Name: &name, AllowSimilar: true}
	p = update(in)
	if !p.Ready || len(p.Diff) != 1 || p.Diff["name"] == nil || len(p.Draft["items"].([]WarehousePackageItem)) != 2 || *p.Draft["price"].(*float64) != 12.34 {
		t.Fatalf("preserving draft: %#v", p)
	}
	version := p.Draft["expected_updated_at"].(string)
	if version != "2026-09-30T10:15:00.123456Z" {
		t.Fatal(version)
	}
	in.ConfirmUpdate = true
	if p = update(in); p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing version: %#v", p)
	}
	in.ExpectedUpdatedAt = "stale"
	if update(in).Ready {
		t.Fatal("stale version must fail")
	}
	in.ExpectedUpdatedAt = version
	in.ClearFields = []string{"description", "price", "category"}
	aliases := []string{}
	in.Aliases = &aliases
	p = update(in)
	if !p.Ready || p.Draft["price"] != (*float64)(nil) || len(p.Diff) != 5 {
		t.Fatalf("clear: %#v", p)
	}
	price := 45.67
	in.Price = &price
	if p = update(in); p.Ready || !containsString(p.Missing, "price") {
		t.Fatalf("conflicting clear: %#v", p)
	}
	in = WarehousePackageUpdateInput{PackageID: 1, Name: &name, AllowSimilar: true}
	exec(`INSERT INTO job_packages VALUES(1,42)`)
	if !update(in).Ready {
		t.Fatal("job use should allow metadata")
	}
	in.Price = &price
	if p = update(in); p.Ready || !containsString(p.Missing, "job_dependencies") {
		t.Fatalf("job price guard: %#v", p)
	}
	in.Price = nil
	items := []WarehousePackageItem{{ProductID: 1, Quantity: 1}}
	in.Items = &items
	if p = update(in); p.Ready || !containsString(p.Missing, "job_dependencies") {
		t.Fatalf("job content guard: %#v", p)
	}
	if p = update(WarehousePackageUpdateInput{PackageID: 2, Name: &name}); p.Ready || !containsString(p.Missing, "package_id") {
		t.Fatalf("inactive guard: %#v", p)
	}
	if p = update(WarehousePackageUpdateInput{PackageID: 1}); p.Ready || !containsString(p.Missing, "changes") {
		t.Fatalf("no-op: %#v", p)
	}
	exec(`UPDATE product_packages SET updated_at=updated_at+interval '1 microsecond' WHERE id=1`)
	if p = update(WarehousePackageUpdateInput{PackageID: 1, Name: &name, ExpectedUpdatedAt: version}); p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("UI invalidation: %#v", p)
	}
}

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

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestWarehousePackageLifecycleAndAudit(t *testing.T) {
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
	const schema = "mcp_package_lifecycle_test"
	if _, err = database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE; CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
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
	exec(`ALTER TABLE job_packages ADD COLUMN job_package_id SERIAL;CREATE TABLE jobs(jobid INT,statusid INT,deleted_at TIMESTAMP);CREATE TABLE status(statusid INT,status TEXT);INSERT INTO jobs VALUES(1,1,NULL),(2,2,NULL),(3,NULL,NULL);INSERT INTO status VALUES(1,'open'),(2,'closed');CREATE TABLE job_package_reservations(job_package_id INT,reservation_status VARCHAR(20));CREATE FUNCTION warehouse_job_status_is_closed(TEXT) RETURNS BOOLEAN LANGUAGE SQL IMMUTABLE AS $$ SELECT COALESCE($1,'')='closed' $$;CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,user_agent TEXT,timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`)
	prepare := func(in WarehousePackageLifecycleInput, op string) preparedMutation {
		t.Helper()
		p, err := run(true, func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehousePackageLifecycle(ctx, db, in, op)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := prepare(WarehousePackageLifecycleInput{PackageID: 1}, "archive")
	if !p.Ready || p.Draft["confirmation_text_required"] != "ARCHIVE WAREHOUSE PACKAGE 1" {
		t.Fatal(p)
	}
	if p := prepare(WarehousePackageLifecycleInput{PackageID: 1, ConfirmLifecycle: true}, "archive"); p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatal("missing version")
	}
	if prepare(WarehousePackageLifecycleInput{PackageID: 1, ExpectedUpdatedAt: "stale"}, "archive").Ready {
		t.Fatal("stale accepted")
	}
	for _, job := range []int{1, 3} {
		exec(`INSERT INTO job_packages(package_id,job_id) VALUES(1,$1)`, job)
		if prepare(WarehousePackageLifecycleInput{PackageID: 1}, "archive").Ready {
			t.Fatal("open or unknown-status job accepted")
		}
		exec(`DELETE FROM job_packages`)
	}
	exec(`INSERT INTO job_packages(package_id,job_id) VALUES(1,2)`)
	if !prepare(WarehousePackageLifecycleInput{PackageID: 1}, "archive").Ready {
		t.Fatal("closed job blocked")
	}
	exec(`INSERT INTO job_package_reservations SELECT job_package_id,'reserved' FROM job_packages`)
	if prepare(WarehousePackageLifecycleInput{PackageID: 1}, "archive").Ready {
		t.Fatal("reservation ignored")
	}
	exec(`UPDATE job_package_reservations SET reservation_status='released'; UPDATE product_packages SET is_active=false,website_visible=true WHERE id=1`)
	p = prepare(WarehousePackageLifecycleInput{PackageID: 1}, "restore")
	if !p.Ready || p.Diff["website_visible"]["after"] != false {
		t.Fatal("restore visibility diff missing")
	}
	exec(`UPDATE products SET lifecycle_status='archived' WHERE productid=2`)
	if prepare(WarehousePackageLifecycleInput{PackageID: 1}, "restore").Ready {
		t.Fatal("inactive product accepted")
	}
	exec(`UPDATE products SET lifecycle_status='active' WHERE productid=2`)
	exec(`DELETE FROM products WHERE productid=2`)
	if prepare(WarehousePackageLifecycleInput{PackageID: 1}, "restore").Ready {
		t.Fatal("missing product accepted")
	}
	exec(`INSERT INTO products VALUES(2,'Beta','active')`)
	if _, err := run(false, func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehousePackageLifecycle(ctx, db, WarehousePackageLifecycleInput{PackageID: 1}, "restore")
	}); err == nil {
		t.Fatal("nonadmin preview accepted")
	}
	exec(`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,user_agent) VALUES(11,'package.archive','package','1','{"name":"Sound Package","description":"secret description","website_visible":true,"is_active":true}','{"origin":"MCP/AI","is_active":false,"updated_at":"2026-09-30T10:15:00.123456Z","after":{"name":"Sound Package","description":"secret description","website_visible":false}}','private-agent')`)
	_, err = run(true, func(ctx context.Context) (preparedMutation, error) {
		data, _, _, err := warehousePackageAuditHistory(ctx, db, WarehousePackageAuditInput{PackageID: 1})
		if err != nil {
			return preparedMutation{}, err
		}
		raw, _ := json.Marshal(data)
		if strings.Contains(string(raw), "secret description") || strings.Contains(string(raw), "private-agent") || strings.Contains(string(raw), "old_values") {
			t.Fatal("audit leaks private fields")
		}
		events := data.(map[string]any)["events"].([]map[string]any)
		if len(events) != 1 || events[0]["result_version"] != "2026-09-30T10:15:00.123456Z" {
			t.Fatal(data)
		}
		return preparedMutation{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"archive", "restore"} {
		if requiredMutationScope("warehouse.packages."+op) != "cores:warehouse:archive" {
			t.Fatal("scope")
		}
	}
	entitySchema := renderWritableEntitySchema(writableEntitySchemas()["warehouse.packages"])
	if len(entitySchema["lifecycle_fields"].([]map[string]any)) != 4 {
		t.Fatal(entitySchema)
	}
}

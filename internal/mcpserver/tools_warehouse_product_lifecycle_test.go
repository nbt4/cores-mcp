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

func TestPrepareWarehouseProductLifecycleShowsDependenciesAndVersion(t *testing.T) {
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
	const schema = "mcp_warehouse_lifecycle_test"
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
		`CREATE TABLE products(productid INT PRIMARY KEY,name TEXT,product_code TEXT,lifecycle_status TEXT,website_visible BOOL,website_featured BOOL,updated_at TIMESTAMP)`,
		`CREATE TABLE devices(deviceid TEXT PRIMARY KEY,productid INT,lifecycle_status TEXT,archived_by_product BOOL)`,
		`CREATE TABLE status(statusid INT PRIMARY KEY,status TEXT)`,
		`CREATE TABLE jobs(jobid INT PRIMARY KEY,statusid INT,deleted_at TIMESTAMP)`,
		`CREATE TABLE job_product_requirements(job_id INT,product_id INT)`,
		// Fixture for the owning Core's read-only job-reference contract. Full
		// ancestor/job validation is exercised by the Warehouse SQL integration tests.
		`CREATE FUNCTION warehouse_relation_active_jobs(pid INT) RETURNS JSONB AS $$ SELECT COALESCE(jsonb_agg(jsonb_build_object('job_id',job_id)),'[]'::jsonb) FROM job_product_requirements WHERE product_id=pid $$ LANGUAGE SQL STABLE`,
		`CREATE TABLE job_devices(jobid INT,deviceid TEXT,pack_status TEXT)`,
		`INSERT INTO products VALUES(1,'Mixer','PRD-1','active',TRUE,TRUE,'2026-09-24T08:00:00.123456')`,
		`INSERT INTO devices VALUES('D-1',1,'active',FALSE)`,
		`INSERT INTO status VALUES(1,'Planung')`,
		`INSERT INTO jobs VALUES(10,1,NULL)`,
		`INSERT INTO job_product_requirements VALUES(10,1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(input WarehouseProductLifecycleInput) (preparedMutation, error) {
		var result preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, resultErr = prepareWarehouseProductLifecycle(request.Context(), db, input, "archive")
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, resultErr
	}
	input := WarehouseProductLifecycleInput{ProductID: 1}
	preview, err := run(input)
	if err != nil || preview.Ready || !containsString(preview.Missing, "active_dependencies") || !containsString(preview.Missing, "active_dependency_jobs") || preview.Draft["expected_updated_at"] != "2026-09-24T08:00:00.123456Z" {
		t.Fatalf("dependency preview: %#v %v", preview, err)
	}
	if _, err := database.Exec(`DELETE FROM job_product_requirements`); err != nil {
		t.Fatal(err)
	}
	input.ConfirmLifecycle = true
	preview, err = run(input)
	if err != nil || preview.Ready || !containsString(preview.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", preview, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:00:00.123456Z"
	preview, err = run(input)
	if err != nil || !preview.Ready || preview.Draft["confirmation_text_required"] != "ARCHIVE WAREHOUSE PRODUCT 1" {
		t.Fatalf("ready preview: %#v %v", preview, err)
	}
}

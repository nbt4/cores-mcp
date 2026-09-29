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

func TestPrepareWarehouseProductRelationVersionAndDiff(t *testing.T) {
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
	const schema = "mcp_warehouse_relation_test"
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
		`CREATE TABLE products(productid INT PRIMARY KEY,name TEXT,product_code TEXT,lifecycle_status TEXT,updated_at TIMESTAMP)`,
		`CREATE TABLE product_dependencies(id SERIAL PRIMARY KEY,product_id INT,dependency_product_id INT,relation_type TEXT,assignment_scope TEXT,default_quantity NUMERIC(10,2),notes TEXT)`,
		`INSERT INTO products VALUES(1,'Mixer','PRD-1','active','2026-09-24T08:00:00.123456'),(2,'Cable','PRD-2','active','2026-09-24T08:00:00.123456')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(input WarehouseProductRelationInput) (preparedMutation, error) {
		var p preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p, resultErr = prepareWarehouseProductRelation(r.Context(), db, input)
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, resultErr
	}
	input := WarehouseProductRelationInput{ProductID: 1, DependencyProductID: 2}
	p, err := run(input)
	if err != nil || !p.Ready || len(p.Diff) == 0 || p.Draft["expectedUpdatedAt"] != "2026-09-24T08:00:00.123456Z" {
		t.Fatalf("create preview: %#v %v", p, err)
	}
	input.ConfirmLink = true
	p, err = run(input)
	if err != nil || p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", p, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:00:00.123456Z"
	p, err = run(input)
	if err != nil || !p.Ready || p.Draft["confirmation_text_required"] != "LINK WAREHOUSE PRODUCT 1 TO 2" {
		t.Fatalf("ready preview: %#v %v", p, err)
	}
	if _, err := database.Exec(`INSERT INTO product_dependencies(product_id,dependency_product_id,relation_type,assignment_scope,default_quantity) VALUES(1,2,'recommended','product',1)`); err != nil {
		t.Fatal(err)
	}
	p, err = run(input)
	if err != nil || p.Ready || !containsString(p.Missing, "changes") {
		t.Fatalf("unchanged relationship accepted: %#v %v", p, err)
	}
}

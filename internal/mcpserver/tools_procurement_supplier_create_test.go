package mcpserver

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestPrepareSupplierCreateValidatesCompleteDraftBeforeDatabaseLookup(t *testing.T) {
	prepared, err := prepareSupplierCreate(context.Background(), nil, SupplierCreateInput{
		Name: " ", Code: "bad code", Website: "file:///etc/passwd", Email: "not-an-email",
		DefaultLeadDays: -1, Rating: 6, RiskLevel: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "code", "website", "email", "default_lead_days", "rating", "risk_level"} {
		if !containsString(prepared.Missing, field) {
			t.Errorf("missing validation for %s: %#v", field, prepared.Missing)
		}
	}
	if prepared.Ready || prepared.Draft["active"] != true {
		t.Fatalf("invalid supplier draft = %#v", prepared)
	}
}

func TestPrepareSupplierCreateChecksLiveDuplicates(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_SUPPLIER_TEST_DATABASE_URL to a disposable PostgreSQL database ending in _test")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("supplier integration test requires a dedicated _test database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_supplier_prepare_test"
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
	if _, err := database.Exec(`CREATE TABLE proc_suppliers (id BIGSERIAL PRIMARY KEY, name TEXT, code TEXT, active BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO proc_suppliers (name,code,active) VALUES ('Acme Lighting','ACME-01',true),('Other Company','OTHER-01',true)`); err != nil {
		t.Fatal(err)
	}
	lookup := store.New(database, 5*time.Second, 200)
	input := SupplierCreateInput{Name: "Acme Lighting GmbH", Code: "NEW-01"}
	prepared, err := prepareSupplierCreate(context.Background(), lookup, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "similar_supplier_review") || len(prepared.RelatedRecords) != 1 || len(prepared.Sources) != 1 {
		t.Fatalf("similar supplier not surfaced: %#v, %v", prepared, err)
	}
	input.AllowSimilar = true
	prepared, err = prepareSupplierCreate(context.Background(), lookup, input)
	if err != nil || !prepared.Ready || len(prepared.RelatedRecords) != 1 {
		t.Fatalf("explicitly reviewed distinct supplier not ready: %#v, %v", prepared, err)
	}
	input.Code = "acme-01"
	prepared, err = prepareSupplierCreate(context.Background(), lookup, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "duplicate_code") {
		t.Fatalf("duplicate supplier code not blocked: %#v, %v", prepared, err)
	}
}

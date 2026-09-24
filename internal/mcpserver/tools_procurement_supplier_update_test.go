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

func TestPrepareSupplierUpdateDiffVersionAndDuplicates(t *testing.T) {
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
	const schema = "mcp_supplier_update_test"
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
	if _, err := database.Exec(`CREATE TABLE proc_suppliers (id BIGSERIAL PRIMARY KEY, name TEXT, code TEXT, website TEXT, contact_name TEXT, email TEXT, phone TEXT, payment_terms TEXT, default_lead_days INTEGER, rating DOUBLE PRECISION, preferred BOOLEAN, active BOOLEAN, risk_level TEXT, notes TEXT, updated_at TIMESTAMPTZ)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE proc_purchase_orders (id BIGSERIAL PRIMARY KEY, supplier_id BIGINT, number TEXT, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO proc_suppliers (name,code,website,contact_name,email,phone,payment_terms,default_lead_days,rating,preferred,active,risk_level,notes,updated_at) VALUES
		('Acme Lighting','ACME-01','','','','','',3,4,false,true,'low','', '2026-09-24T08:15:00.123456Z'),
		('Acme International','ACME-02','','','','','',0,0,false,true,'low','', '2026-09-24T08:15:00Z')`); err != nil {
		t.Fatal(err)
	}
	lookup := store.New(database, 5*time.Second, 200)
	name := "Acme Lighting International"
	input := SupplierUpdateInput{SupplierID: 1, Name: &name}
	run := func(admin bool, value SupplierUpdateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareSupplierUpdate(request.Context(), lookup, value)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin read of supplier details was accepted")
	}
	prepared, err := run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "similar_supplier_review") || len(prepared.Diff) != 1 || len(prepared.RelatedRecords) != 1 {
		t.Fatalf("similar update preview: %#v, %v", prepared, err)
	}
	input.AllowSimilar = true
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready || prepared.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" {
		t.Fatalf("reviewed update preview: %#v, %v", prepared, err)
	}
	input.ConfirmUpdate = true
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00Z"
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "expected_updated_at") {
		t.Fatalf("stale supplier version accepted: %#v, %v", prepared, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready {
		t.Fatalf("matching supplier version rejected: %#v, %v", prepared, err)
	}
	duplicate := "ACME-02"
	input.Code = &duplicate
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "duplicate_code") {
		t.Fatalf("duplicate supplier code accepted: %#v, %v", prepared, err)
	}
	if _, err := database.Exec(`INSERT INTO proc_purchase_orders (supplier_id,number,status) VALUES (1,'PO-001','sent')`); err != nil {
		t.Fatal(err)
	}
	inactive := false
	input = SupplierUpdateInput{SupplierID: 1, Active: &inactive}
	prepared, err = run(true, input)
	if err != nil || prepared.Ready || !containsString(prepared.Missing, "open_orders") || len(prepared.RelatedRecords) != 1 {
		t.Fatalf("open order did not block deactivation: %#v, %v", prepared, err)
	}
	if _, err := database.Exec(`DELETE FROM proc_purchase_orders`); err != nil {
		t.Fatal(err)
	}
	prepared, err = run(true, input)
	if err != nil || !prepared.Ready || prepared.Diff["active"]["after"] != false {
		t.Fatalf("completed orders still block deactivation: %#v, %v", prepared, err)
	}
}

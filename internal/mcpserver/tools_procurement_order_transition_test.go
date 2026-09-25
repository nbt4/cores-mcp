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

func TestPrepareOrderTransitionVersionAndStatus(t *testing.T) {
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
	const schema = "mcp_order_transition_test"
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
		`CREATE TABLE proc_purchase_orders(id BIGINT PRIMARY KEY,number TEXT,status TEXT,supplier_id BIGINT,supplier_order_number TEXT,expected_delivery TIMESTAMPTZ,notes TEXT,total_cents BIGINT,updated_at TIMESTAMPTZ)`,
		`CREATE TABLE proc_purchase_order_lines(id BIGINT PRIMARY KEY,purchase_order_id BIGINT,product_id BIGINT,description TEXT,quantity DOUBLE PRECISION,received_quantity DOUBLE PRECISION,unit TEXT,unit_price_cents BIGINT)`,
		`INSERT INTO proc_purchase_orders VALUES(7,'PO-7','draft',3,'S-7',NULL,'Original',2000,'2026-09-24T08:15:00.123456Z')`,
		`INSERT INTO proc_purchase_order_lines VALUES(9,7,NULL,'Cable',2,0,'Stk.',1000)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool, input OrderTransitionInput) (preparedMutation, error) {
		var p preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "41", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p, prepareErr = prepareOrderTransition(r.Context(), db, input)
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, prepareErr
	}
	input := OrderTransitionInput{OrderID: 7, Status: "sent"}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin transition preview accepted")
	}
	p, err := run(true, input)
	if err != nil || !p.Ready || p.Draft["confirmation_text_required"] != "SEND ORDER 7" || len(p.RelatedRecords) != 1 {
		t.Fatalf("send preview: %#v %v", p, err)
	}
	input.ConfirmTransition = true
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", p, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	p, err = run(true, input)
	if err != nil || !p.Ready {
		t.Fatalf("matching version rejected: %#v %v", p, err)
	}
	input.Status = "confirmed"
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "status") {
		t.Fatalf("invalid transition accepted: %#v %v", p, err)
	}
	input.Status = "cancelled"
	p, err = run(true, input)
	if err != nil || p.Ready || !containsString(p.Missing, "reason") {
		t.Fatalf("cancellation without reason accepted: %#v %v", p, err)
	}
	input.Reason = "Supplier cannot deliver"
	p, err = run(true, input)
	if err != nil || !p.Ready || p.Draft["confirmation_text_required"] != "CANCEL ORDER 7" || !strings.Contains(p.Draft["notes"].(string), input.Reason) {
		t.Fatalf("cancel preview: %#v %v", p, err)
	}
}

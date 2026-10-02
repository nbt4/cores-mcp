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

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestPrepareRequisitionCreateUpdateAndSubmit(t *testing.T) {
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
	const schema = "mcp_requisition_test"
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
		`CREATE TABLE proc_products (id BIGINT PRIMARY KEY,name TEXT,active BOOLEAN)`,
		`CREATE TABLE proc_suppliers (id BIGINT PRIMARY KEY,name TEXT,active BOOLEAN)`,
		`CREATE TABLE proc_requisitions (id BIGINT PRIMARY KEY,number TEXT,title TEXT,status TEXT,requester_id BIGINT,cost_center TEXT,justification TEXT,needed_by TIMESTAMPTZ,estimated_total_cents BIGINT,updated_at TIMESTAMPTZ,is_archived BOOLEAN NOT NULL DEFAULT false)`,
		`CREATE TABLE proc_requisition_lines (id BIGINT PRIMARY KEY,requisition_id BIGINT,product_id BIGINT,description TEXT,quantity DOUBLE PRECISION,unit TEXT,estimated_price_cents BIGINT,preferred_supplier_id BIGINT,purchase_url TEXT)`,
		`INSERT INTO proc_products VALUES (1,'DMX cable',true),(2,'Retired cable',false)`,
		`INSERT INTO proc_suppliers VALUES (3,'Stage Supply',true)`,
		`INSERT INTO proc_requisitions VALUES (7,'BAN-7','Tour cable','draft',41,'EVENT','Needed for tour',NULL,2500,'2026-09-24T08:15:00.123456Z')`,
		`INSERT INTO proc_requisition_lines VALUES (9,7,1,'DMX cable',2,'Stk.',1250,3,'')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(user string, admin bool, fn func(context.Context) (preparedMutation, error)) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: user, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { result, prepareErr = fn(r.Context()) })).ServeHTTP(httptest.NewRecorder(), func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Bearer test")
			return r
		}())
		return result, prepareErr
	}
	lines := []RequisitionLineInput{{ProductID: func() *int64 { v := int64(1); return &v }(), Description: "DMX cable", Quantity: 2, EstimatedPriceCents: 1250}}
	title := "Tour cable"
	create := RequisitionDraftInput{Title: &title, Lines: &lines}
	p, err := run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, create, false)
	})
	if err != nil || !p.Ready || len(p.RelatedRecords) != 1 {
		t.Fatalf("create preview: %#v %v", p, err)
	}
	lines[0].ProductID = func() *int64 { v := int64(2); return &v }()
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, create, false)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "lines[0].product_id") {
		t.Fatalf("inactive product accepted: %#v %v", p, err)
	}
	newTitle := "Tour cable urgent"
	update := RequisitionDraftInput{RequisitionID: 7, Title: &newTitle}
	if _, err = run("42", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	}); err == nil {
		t.Fatal("another requester could edit")
	}
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	})
	if err != nil || !p.Ready || len(p.Diff) != 1 || p.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" {
		t.Fatalf("update preview: %#v %v", p, err)
	}
	update.ConfirmUpdate = true
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v %v", p, err)
	}
	update.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	})
	if err != nil || !p.Ready {
		t.Fatalf("matching version rejected: %#v %v", p, err)
	}
	if _, err := database.Exec("UPDATE proc_requisitions SET status='returned' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	})
	if err != nil || !p.Ready || p.Diff["status"]["before"] != "returned" || p.Diff["status"]["after"] != "draft" {
		t.Fatal("missing returned revision diff", p, err)
	}
	if _, err := database.Exec("UPDATE proc_requisitions SET is_archived=true WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) {
		return prepareRequisitionDraft(ctx, db, update, true)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "active_requisition") {
		t.Fatal("archived revision accepted", p, err)
	}
	if _, err := database.Exec("UPDATE proc_requisitions SET is_archived=false,status='draft' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	submit := RequisitionSubmitInput{RequisitionID: 7, ConfirmSubmit: true}
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) { return prepareRequisitionSubmit(ctx, db, submit) })
	if err != nil || p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("submit missing version accepted: %#v %v", p, err)
	}
	submit.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	p, err = run("41", false, func(ctx context.Context) (preparedMutation, error) { return prepareRequisitionSubmit(ctx, db, submit) })
	if err != nil || !p.Ready || p.Draft["confirmation_text_required"] != "SUBMIT REQUISITION 7" {
		t.Fatalf("submit preview: %#v %v", p, err)
	}
	// Historical native and retained lifecycle audits must both expose their
	// precise result version without leaking notes or raw before/after JSON.
	if _, err := database.Exec(`CREATE TABLE users(userid BIGINT PRIMARY KEY,is_active BOOLEAN,is_admin BOOLEAN);
INSERT INTO users VALUES(41,true,false),(42,true,false),(43,true,true);
CREATE TABLE audit_log(id BIGINT PRIMARY KEY,action TEXT,entity_type TEXT,entity_id TEXT,user_id BIGINT,timestamp TIMESTAMPTZ,old_values JSONB,new_values JSONB);
INSERT INTO audit_log VALUES
(1,'requisition.updated','procurement_requisition','7',41,now()-interval '1 minute','{"status":"returned","justification":"private old note"}','{"origin":"MCP/AI","requisition":{"status":"draft","updatedAt":"2026-10-03T08:00:00.000123Z","estimatedTotalCents":100,"justification":"private new note"}}'),
(2,'requisition.restore','procurement_requisition','7',41,now(),'{"isArchived":true,"status":"draft"}','{"origin":"MCP/AI","after":{"isArchived":false,"status":"draft","updatedAt":"2026-10-03T08:00:00.000124Z","estimatedTotalCents":100}}');`); err != nil {
		t.Fatal(err)
	}
	var history any
	readHistory := func(ctx context.Context) (preparedMutation, error) {
		var historyErr error
		history, _, _, historyErr = procurementAuditHistory(ctx, db, ProcurementAuditInput{Entity: "requisition", ID: 7})
		return preparedMutation{}, historyErr
	}
	if _, err := run("41", false, readHistory); err != nil {
		t.Fatal(err)
	}
	events := history.(map[string]any)["events"].([]map[string]any)
	if len(events) != 2 || events[0]["result_version"] != "2026-10-03T08:00:00.000124Z" || events[1]["result_version"] != "2026-10-03T08:00:00.000123Z" || events[0]["total_cents_after"] != "100" {
		t.Fatal("missing historical/lifecycle result version or retained total", history)
	}
	encoded, err := json.Marshal(history)
	if err != nil || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "justification") {
		t.Fatal("unredacted history", string(encoded), err)
	}
	if _, err := run("42", false, readHistory); err == nil {
		t.Fatal("unrelated requester read history")
	}
	if _, err := database.Exec("UPDATE users SET is_admin=false WHERE userid=43"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("43", true, readHistory); err == nil {
		t.Fatal("revoked administrator read history using stale claims")
	}

}

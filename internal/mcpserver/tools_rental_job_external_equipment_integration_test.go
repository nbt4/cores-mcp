package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestExternalRentalAssignmentReferenceResolution(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable _test PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("dedicated _test DB required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`DROP SCHEMA IF EXISTS mcp_external_rental_reference_test CASCADE;CREATE SCHEMA mcp_external_rental_reference_test;SET search_path TO mcp_external_rental_reference_test;
 CREATE TABLE jobs(jobid INT,job_code TEXT,description TEXT,deleted_at TIMESTAMP);
 INSERT INTO jobs VALUES(1,'JOB_TEST_A','Synthetic rental job',NULL),(2,'JOB_TEST_B','Other job',NULL),(3,'JOB_ARCHIVED','Archived',NOW());
 CREATE TABLE rental_equipment(id INT,name TEXT,supplier TEXT,category TEXT,is_active BOOL);
 INSERT INTO rental_equipment VALUES(10,'Synthetic rental','Supplier A','Lighting',true),(11,'Synthetic rental','Supplier B','Lighting',true),(12,'Inactive rental','Supplier A','Lighting',false)`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP SCHEMA IF EXISTS mcp_external_rental_reference_test CASCADE`)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := []map[string]any{}
	http.DefaultTransport = inventoryTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, body)
		if body["job_id"] != float64(1) || body["equipment_id"] != float64(10) || body["preview"] != true {
			t.Fatal("wrong resolved owner draft", body)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"operation_status":"confirmation_required","draft":{"job_id":1,"equipment_id":10}}`)), Request: r}, nil
	})
	ctx := inventoryTestContext(t, "42", true, "cores:rental:create", "cores:rental:financial")
	ctx = withMutationPermission(ctx, "cores:rental:create")
	cfg := config.Config{RentalURL: "http://rental.invalid", JWTSecret: strings.Repeat("reference-test-", 4)}
	run := func(in RentalJobExternalEquipmentInput) map[string]any {
		t.Helper()
		data, _, _, err := invokeRentalJobExternalEquipment(context.WithoutCancel(ctx), cfg, store.New(db, 5*time.Second, 200), in, true)
		if err != nil {
			t.Fatal(err)
		}
		return data.(map[string]any)
	}
	p := run(RentalJobExternalEquipmentInput{JobQuery: "JOB_TEST_A", EquipmentQuery: "Synthetic rental"})
	if p["ready_to_execute"] != false || len(p["candidates"].([]map[string]any)) != 2 || len(calls) != 0 {
		t.Fatal("ambiguous equipment guessed", p)
	}
	for _, in := range []RentalJobExternalEquipmentInput{
		{JobQuery: "JOB_ARCHIVED", EquipmentID: 10},
		{JobQuery: "JOB_TEST_A", EquipmentQuery: "Inactive rental"},
		{JobQuery: "JOB_TEST", EquipmentID: 10},
	} {
		p := run(in)
		if p["ready_to_execute"] != false || len(calls) != 0 {
			t.Fatal("inactive or ambiguous reference accepted", p)
		}
	}
	run(RentalJobExternalEquipmentInput{JobQuery: "JOB_TEST_A", EquipmentID: 10, EquipmentQuery: "Synthetic rental"})
	if len(calls) != 1 {
		t.Fatal("exact equipment identity was not resolved")
	}
}

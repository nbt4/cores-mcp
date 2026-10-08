package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	// The ordinary job context exposes canonical rental positions, separate from supplier costs.
	_, err = db.Exec(`
      ALTER TABLE jobs ADD COLUMN customerid INT,ADD COLUMN statusid INT,ADD COLUMN startdate DATE,ADD COLUMN enddate DATE,ADD COLUMN venue_id INT,ADD COLUMN revenue NUMERIC DEFAULT 119,ADD COLUMN final_revenue NUMERIC DEFAULT 119,ADD COLUMN discount NUMERIC DEFAULT 0,ADD COLUMN discount_type TEXT DEFAULT 'amount',ADD COLUMN jobcategoryid INT,ADD COLUMN multiply_by_days BOOL DEFAULT true,ADD COLUMN prices_include_tax BOOL DEFAULT false,ADD COLUMN revision INT DEFAULT 1,ADD COLUMN updated_at TIMESTAMP DEFAULT NOW();
      CREATE TABLE customers(customerid INT PRIMARY KEY,companyname TEXT,name TEXT,firstname TEXT,lastname TEXT);
      CREATE TABLE status(statusid INT PRIMARY KEY,status TEXT);
      CREATE TABLE venues(id INT PRIMARY KEY,name TEXT,city TEXT);
      CREATE TABLE products(productid INT PRIMARY KEY,name TEXT,product_code TEXT,tracking_mode TEXT);
      CREATE TABLE job_product_requirements(id INT,job_id INT,product_id INT,quantity INT);
      CREATE TABLE job_devices(jobid INT,deviceid TEXT);
      CREATE TABLE devices(deviceid TEXT,productid INT);
      CREATE TABLE product_packages(id INT,name TEXT);
      CREATE TABLE job_packages(job_package_id INT,job_id INT,package_id INT,quantity INT,custom_price NUMERIC,notes TEXT);
      ALTER TABLE rental_equipment ADD COLUMN rental_price NUMERIC DEFAULT 37.50,ADD COLUMN customer_price NUMERIC DEFAULT 50;
      CREATE TABLE job_positions(position_id INT,job_id INT,position_type TEXT,rental_equipment_id INT,description TEXT,quantity NUMERIC,unit TEXT,unit_price NUMERIC,follow_day_factor NUMERIC,discount_percent NUMERIC DEFAULT 0,discount_amount NUMERIC DEFAULT 0,tax_rate NUMERIC DEFAULT 19,sort_order INT DEFAULT 0,deleted_at TIMESTAMP);
      INSERT INTO job_positions(position_id,job_id,position_type,rental_equipment_id,description,quantity,unit,unit_price,follow_day_factor) VALUES(1,1,'rental',10,'Synthetic rental',2,'Stück',50,0);
      CREATE TABLE job_rental_equipment(job_id INT,equipment_id INT,position_id INT,quantity INT,days_used INT,total_cost NUMERIC);
      INSERT INTO job_rental_equipment VALUES(1,10,1,2,1,75);
      CREATE TABLE employees(id INT,first_name TEXT,last_name TEXT);
      CREATE TABLE job_employees(job_id INT,employee_id INT,role TEXT);
    `)
	if err != nil {
		t.Fatal(err)
	}
	repository := store.New(db, 5*time.Second, 200)
	data, sources, _, err := rentalJobContext(ctx, repository, IDInput{ID: "JOB_TEST_A"})
	if err != nil {
		t.Fatal(err)
	}
	contextData := data.(map[string]any)
	positions := contextData["rental_positions"].([]map[string]any)
	costs := contextData["external_rentals"].([]map[string]any)
	if len(positions) != 1 || len(costs) != 1 || len(sources) == 0 || fmt.Sprint(positions[0]["unit_price"]) != "50" || fmt.Sprint(costs[0]["total_cost"]) != "75" || costs[0]["repair_required"] != false {
		t.Fatal("normal job read conflates sales and costs", contextData)
	}
	if _, _, _, err := rentalJobContext(inventoryTestContext(t, "42", true, "cores:read"), repository, IDInput{ID: "JOB_TEST_A"}); err == nil {
		t.Fatal("financial read scope bypass")
	}

}

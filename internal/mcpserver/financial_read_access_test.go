package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

func financialTestContext(t *testing.T, scopes []string) context.Context {
	t.Helper()
	var result context.Context
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "43", Scopes: scopes, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { result = r.Context() }))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer fixture")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if result == nil {
		t.Fatal("test identity missing")
	}
	return result
}

func TestFinancialReadProjectionSeparatesServicesAndPreservesData(t *testing.T) {
	original := map[string]any{"product_id": 1, "quantity": 2, "price_per_unit": "123.45", "asset_revenue": "321.00", "best_price_cents": 1000, "job": map[string]any{"revenue": "888.88", "quantity": 3}, "rows": []map[string]any{{"total_cents": 222, "unit": "pcs"}}, "nested": []any{map[string]any{"custom_price": "77.77", "device_id": "D1"}}}
	for _, tc := range []struct {
		name   string
		scopes []string
		want   []string
		deny   []string
	}{
		{"no scopes", nil, []string{"product_id", "quantity", "device_id"}, []string{"price_per_unit", "asset_revenue", "best_price_cents", "revenue", "total_cents", "custom_price"}},
		{"legacy write", []string{"cores:read", "cores:write"}, []string{"product_id"}, []string{"price_per_unit", "best_price_cents", "revenue"}},
		{"warehouse only", []string{warehouseFinancialScope}, []string{"price_per_unit", "asset_revenue"}, []string{"best_price_cents", "total_cents", "revenue", "custom_price"}},
		{"rental only", []string{rentalFinancialScope}, []string{"revenue", "custom_price"}, []string{"price_per_unit", "asset_revenue", "best_price_cents", "total_cents"}},
		{"procurement only", []string{procurementFinancialScope}, []string{"best_price_cents", "total_cents"}, []string{"price_per_unit", "asset_revenue", "revenue", "custom_price"}},
		{"all", []string{warehouseFinancialScope, rentalFinancialScope, procurementFinancialScope}, []string{"price_per_unit", "asset_revenue", "best_price_cents", "revenue", "total_cents", "custom_price"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := prettyJSON(original)
			result, warnings := projectFinancialReadData(financialTestContext(t, tc.scopes), "equipment.strategy.context", original)
			encoded := prettyJSON(result)
			for _, key := range tc.want {
				if !strings.Contains(encoded, `"`+key+`"`) {
					t.Fatalf("missing allowed %s", key)
				}
			}
			for _, key := range tc.deny {
				if strings.Contains(encoded, `"`+key+`"`) {
					t.Fatalf("leaked %s", key)
				}
			}
			if len(tc.deny) > 0 && len(warnings) == 0 {
				t.Fatal("missing omitted finance disclosure")
			}
			if prettyJSON(original) != before {
				t.Fatal("projection changed original shared data")
			}
			if tc.name == "all" && (!reflect.DeepEqual(original, result) || len(warnings) > 0) {
				t.Fatal("granted data changed")
			}
		})
	}
	for _, tool := range []string{"cores.entities.schema", "cores.query.catalog", "cores.query.records", "cores.query.aggregate"} {
		result, warnings := projectFinancialReadData(context.Background(), tool, original)
		if !reflect.DeepEqual(result, original) || len(warnings) != 0 {
			t.Fatal("discovery metadata or validated query aliases rewritten")
		}
	}
}

// Query authorization must reject inference through filters/sort/joins/metrics,
// while keeping stock facts available and price-ranked identities absent. This
// test uses the real HTTP MCP transport and a single pooled PG connection.
func TestFinancialReadAccessAcrossMCPQueriesAndOffers(t *testing.T) {
	raw := os.Getenv("CORES_MCP_PROCUREMENT_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set CORES_MCP_PROCUREMENT_TEST_DATABASE_URL to an isolated _test database")
	}
	target, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(target.Path, "_test") {
		t.Fatal("an isolated _test database is required")
	}
	adminDB, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := fmt.Sprintf("mcp_finance_read_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer adminDB.Exec("DROP SCHEMA " + schema + " CASCADE")
	options := target.Query()
	options.Set("search_path", schema)
	target.RawQuery = options.Encode()
	database, err := sql.Open("pgx", target.String())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(procurementReadAccessFixture + `
 ALTER TABLE products ADD product_type TEXT,ADD product_kind TEXT,ADD is_accessory BOOLEAN,ADD is_consumable BOOLEAN,ADD price_per_unit NUMERIC;
 UPDATE products SET price_per_unit=123.45;
 ALTER TABLE devices ADD zone_id BIGINT,ADD current_case_id BIGINT;
 ALTER TABLE jobs ADD customerid BIGINT,ADD venue_id BIGINT,ADD revenue NUMERIC,ADD final_revenue NUMERIC;
 CREATE TABLE customers(customerid BIGINT PRIMARY KEY,companyname TEXT,name TEXT,firstname TEXT,lastname TEXT);
 ALTER TABLE proc_purchase_order_lines ADD description TEXT,ADD unit TEXT;
 ALTER TABLE proc_purchase_orders ADD total_cents BIGINT,ADD ordered_by_name TEXT,ADD is_archived BOOLEAN;
 INSERT INTO proc_suppliers(id,name,preferred,rating,risk_level,active) VALUES(1,'Cheap slow supplier',false,5,'low',true),(2,'Expensive fast supplier',false,5,'low',true);
 INSERT INTO proc_offers VALUES(1,1,1,'CHEAP',100,'EUR',1,1,10,'',now()-interval '1 day',now(),true),(2,1,2,'EXPENSIVE',900,'EUR',1,1,1,'',now()+interval '1 day',now(),true);
 INSERT INTO proc_price_histories VALUES(1,1,100,'EUR',now());
 CREATE TABLE rental_equipment(id BIGINT PRIMARY KEY,name TEXT,supplier TEXT,category TEXT,description TEXT,rental_price NUMERIC,customer_price NUMERIC,is_active BOOL);
 CREATE TABLE job_rental_equipment(job_id BIGINT,equipment_id BIGINT,total_cost NUMERIC);
 INSERT INTO rental_equipment VALUES(1,'External microphone','Synthetic Rental Supplier','Audio','Fixture',12.35,21.50,true);
 INSERT INTO job_rental_equipment VALUES(1,1,37.05);
 CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,timestamp TIMESTAMPTZ DEFAULT now());
 INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values)
 SELECT 43,'fixture.changed',entity,'1','{"priceCents":900,"totalCents":1000}'::jsonb,'{"origin":"MCP/AI","after":{"priceCents":700,"totalCents":800}}'::jsonb
 FROM unnest(ARRAY['procurement_supplier','procurement_product','procurement_offer','procurement_category','procurement_order','procurement_requisition']) AS entity;
 INSERT INTO jobs(jobid,job_code,description,startdate,enddate,revenue,final_revenue) VALUES(1,'JOB','Job',now(),now(),777.88,888.99);
 `); err != nil {
		t.Fatal(err)
	}
	repository := store.New(database, 5*time.Second, 200)
	server := mcp.NewServer(&mcp.Implementation{Name: "finance-read-test", Version: "1"}, nil)
	registerQueryTools(server, repository)
	registerProcurementTools(server, repository)
	registerRentalTools(server, repository)
	registerProcurementAuditTool(server, repository)
	registerProcurementMasterLifecycleTools(server, config.Config{}, repository)
	registerProcurementWorkflowLifecycleTools(server, config.Config{}, repository)
	scopeSets := map[string][]string{"none": {"cores:read"}, "write": {"cores:read", "cores:write"}, "warehouse": {"cores:read", warehouseFinancialScope}, "rental": {"cores:read", rentalFinancialScope}, "procurement": {"cores:read", procurementFinancialScope}, "all": {"cores:read", warehouseFinancialScope, rentalFinancialScope, procurementFinancialScope}}
	verifier := func(_ context.Context, subject string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "43", Scopes: scopeSets[subject], Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
	}
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	httpServer := httptest.NewServer(auth.RequireBearerToken(verifier, nil)(transport))
	defer httpServer.Close()
	sessions := map[string]*mcp.ClientSession{}
	for key := range scopeSets {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: key}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		sessions[key] = session
	}
	call := func(scope, name string, args any, wantError bool) Output {
		t.Helper()
		result, err := sessions[scope].CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s/%s error=%v: %s", scope, name, result.IsError, encoded)
		}
		var output Output
		if err := json.Unmarshal(encoded, &output); err != nil {
			t.Fatal(err)
		}
		return output
	}
	for _, tc := range []struct{ entity, field, scope string }{{"warehouse.products", "price_per_unit", "warehouse"}, {"rental.jobs", "revenue", "rental"}, {"procurement.offers", "price_cents", "procurement"}, {"procurement.requisitions", "estimated_total_cents", "procurement"}, {"procurement.requisition_lines", "estimated_price_cents", "procurement"}, {"procurement.orders", "total_cents", "procurement"}, {"procurement.order_lines", "unit_price_cents", "procurement"}} {
		for _, scope := range []string{"none", "write", "warehouse", "rental", "procurement", "all"} {
			denied := scope != tc.scope && scope != "all"
			projection := map[string]any{"queries": []map[string]any{{"alias": "records", "entity": tc.entity, "fields": []string{tc.field}}}}
			call(scope, "cores.query.records", projection, denied)
			call(scope, "cores.query.records", map[string]any{"queries": []map[string]any{{"alias": "records", "entity": tc.entity, "filters": []map[string]any{{"field": tc.field, "operator": "gt", "value": "0"}}}}}, denied)
			call(scope, "cores.query.records", map[string]any{"queries": []map[string]any{{"alias": "records", "entity": tc.entity, "sort": []map[string]any{{"field": tc.field}}}}}, denied)
			call(scope, "cores.query.aggregate", map[string]any{"entity": tc.entity, "metrics": []map[string]any{{"function": "sum", "field": tc.field}}}, denied)
			call(scope, "cores.query.aggregate", map[string]any{"entity": tc.entity, "group_by": []string{tc.field}}, denied)
		}
	}
	joins := map[string]any{"queries": []map[string]any{{"alias": "left", "entity": "warehouse.products", "fields": []string{"product_id"}}, {"alias": "right", "entity": "procurement.offers", "fields": []string{"offer_id"}}}, "joins": []map[string]any{{"alias": "priced", "left": "left", "right": "right", "left_field": "price_per_unit", "right_field": "price_cents"}}}
	for _, scope := range []string{"none", "warehouse", "procurement"} {
		call(scope, "cores.query.records", joins, true)
	}
	call("all", "cores.query.records", joins, false)
	defaults := call("none", "cores.query.records", map[string]any{"queries": []map[string]any{{"alias": "offers", "entity": "procurement.offers"}, {"alias": "jobs", "entity": "rental.jobs"}}}, false)
	if encoded := prettyJSON(defaults.Data); strings.Contains(encoded, `"price_cents"`) || strings.Contains(encoded, `"revenue"`) {
		t.Fatalf("default financial fields leaked: %s", encoded)
	}
	if len(defaults.Warnings) == 0 {
		t.Fatal("default financial omission not disclosed")
	}
	product := call("none", "procurement.products.get", map[string]any{"id": "1"}, false).Data.(map[string]any)
	if len(product["price_history"].([]any)) != 0 || product["offers"].([]any)[0].(map[string]any)["supplier"] != "Expensive fast supplier" || strings.Contains(prettyJSON(product), `"price_cents"`) {
		t.Fatal("ungranted price history/order leaked")
	}
	priced := call("procurement", "procurement.products.get", map[string]any{"id": "1"}, false).Data.(map[string]any)
	if len(priced["price_history"].([]any)) != 1 || priced["offers"].([]any)[0].(map[string]any)["supplier"] != "Cheap slow supplier" {
		t.Fatal("granted price context missing")
	}
	// Alternate after a financial request on the same connection: the following
	// reader must not inherit the best-price supplier or its monetary fields.
	reorder := call("none", "procurement.reorder.candidates", map[string]any{}, false).Data.([]any)
	if len(reorder) != 1 || reorder[0].(map[string]any)["supplier"] != nil || strings.Contains(prettyJSON(reorder), `"price_cents"`) {
		t.Fatal("pooled best-price identity leaked")
	}
	if got := call("procurement", "procurement.reorder.candidates", map[string]any{}, false).Data.([]any); got[0].(map[string]any)["supplier"] != "Cheap slow supplier" {
		t.Fatal("granted ranked offer missing")
	}
	for _, scope := range []string{"none", "write", "warehouse", "rental"} {
		for _, tool := range []string{"procurement.offers.compare", "procurement.prices.history", "procurement.spend.summary"} {
			call(scope, tool, map[string]any{}, true)
		}
	}
	for _, tool := range []string{"procurement.offers.compare", "procurement.prices.history", "procurement.spend.summary"} {
		call("procurement", tool, map[string]any{}, false)
	}
	// All public history aliases must apply the same financial rule, including
	// exact price and total before/after fields on nested events.
	for _, alias := range []string{"suppliers", "products", "offers", "categories", "orders", "requisitions"} {
		for _, scope := range []string{"none", "write", "warehouse", "rental", "procurement", "all"} {
			data := call(scope, "procurement."+alias+".audit_history", map[string]any{"id": 1}, false).Data
			encoded := prettyJSON(data)
			for _, field := range []string{"price_cents_before", "price_cents_after", "total_cents_before", "total_cents_after"} {
				present := strings.Contains(encoded, `"`+field+`"`)
				if present != (scope == "procurement" || scope == "all") {
					t.Fatalf("audit alias %s scope %s field %s mismatch: %s", alias, scope, field, encoded)
				}
			}
			if !strings.Contains(encoded, "fixture.changed") {
				t.Fatal("operational audit history lost", encoded)
			}
		}
	}
	// External catalog prices belong to Rental, independently of Warehouse.
	for _, scope := range []string{"none", "write", "warehouse", "rental", "procurement", "all"} {
		data := call(scope, "rental.external_equipment.list", map[string]any{}, false).Data
		encoded := prettyJSON(data)
		for _, field := range []string{"rental_price", "customer_price", "historic_cost"} {
			if strings.Contains(encoded, `"`+field+`"`) != (scope == "rental" || scope == "all") {
				t.Fatalf("rental catalog %s/%s: %s", scope, field, encoded)
			}
		}
		if !strings.Contains(encoded, "External microphone") || !strings.Contains(encoded, "job_uses") {
			t.Fatal("operational catalog facts lost", encoded)
		}
	}
	risk := prettyJSON(call("none", "procurement.risks.list", map[string]any{}, false).Data)
	if strings.Contains(risk, "price_cents=") {
		t.Fatal("embedded risk price leaked")
	}
	rows, err := repository.Query(context.Background(), `SELECT current_setting('cores.procurement_financial',true) AS financial,current_setting('cores.user_id',true) AS identity`)
	if err != nil || rows[0]["financial"] != "false" || rows[0]["identity"] != "" {
		t.Fatalf("transaction-local financial access leaked: %v %v", rows, err)
	}
}

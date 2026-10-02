package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestProcurementReadRoundingWithFloatingQuantities(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_PROCUREMENT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_PROCUREMENT_TEST_DATABASE_URL to a disposable _test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		t.Fatal("a disposable _test database is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const testSchema = "mcp_procurement_read_rounding_test"
	if _, err = database.Exec("DROP SCHEMA IF EXISTS " + testSchema + " CASCADE;CREATE SCHEMA " + testSchema + ";SET search_path TO " + testSchema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + testSchema + " CASCADE")
	if _, err = database.Exec(`
CREATE TABLE proc_products(id BIGINT PRIMARY KEY,sku TEXT,name TEXT,manufacturer TEXT,model TEXT,active BOOLEAN);
CREATE TABLE proc_suppliers(id BIGINT PRIMARY KEY,name TEXT,preferred BOOLEAN,rating DOUBLE PRECISION,risk_level TEXT,active BOOLEAN);
CREATE TABLE proc_offers(id BIGINT PRIMARY KEY,product_id BIGINT,supplier_id BIGINT,supplier_sku TEXT,price_cents BIGINT,currency TEXT,minimum_quantity DOUBLE PRECISION,pack_size DOUBLE PRECISION,lead_days INTEGER,valid_until TIMESTAMP,last_checked_at TIMESTAMP,purchase_url TEXT,active BOOLEAN);
CREATE TABLE proc_purchase_orders(id BIGINT PRIMARY KEY,number TEXT,status TEXT,supplier_id BIGINT,currency TEXT,total_cents BIGINT,ordered_by_name TEXT,order_date TIMESTAMP,expected_delivery TIMESTAMP,created_at TIMESTAMP,updated_at TIMESTAMP);
CREATE TABLE proc_purchase_order_lines(id BIGINT PRIMARY KEY,purchase_order_id BIGINT,quantity DOUBLE PRECISION,received_quantity DOUBLE PRECISION);
INSERT INTO proc_products VALUES(1,'ROUND','Rounding fixture','','',true);
INSERT INTO proc_suppliers VALUES(1,'Rounding supplier',false,3,'low',true);
INSERT INTO proc_offers(id,product_id,supplier_id,price_cents,pack_size,active) VALUES(1,1,1,100,3,true),(2,1,1,100,0,true);
INSERT INTO proc_purchase_orders(id,number,status,supplier_id,currency,total_cents,created_at) VALUES(1,'ROUND-1','partially_received',1,'EUR',100,now()),(2,'ROUND-2','draft',1,'EUR',0,now());
INSERT INTO proc_purchase_order_lines VALUES(1,1,3,1),(2,2,0,0);
`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repository := store.New(database, 5*time.Second, 100)
	server := mcp.NewServer(&mcp.Implementation{Name: "rounding-test", Version: "1"}, nil)
	registerProcurementTools(server, repository)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "rounding-client", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	for _, tc := range []struct {
		tool, field string
		want        float64
	}{
		{"procurement.offers.compare", "price_per_unit_cents", 33.33},
		{"procurement.orders.list", "received_percent", 33.3},
	} {
		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: map[string]any{"query": "", "limit": 100}})
		if err != nil || result.IsError {
			t.Fatalf("%s failed: %v, %#v", tc.tool, err, result)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var output struct{ Data []map[string]any }
		if err := json.Unmarshal(raw, &output); err != nil {
			t.Fatal(err)
		}
		if len(output.Data) != 2 {
			t.Fatalf("%s rows: %s", tc.tool, raw)
		}
		found, zero := false, false
		for _, row := range output.Data {
			if row[tc.field] == nil {
				zero = true
			} else if numericFloat(row[tc.field]) == tc.want {
				found = true
			}
		}
		if !found || !zero {
			t.Fatalf("%s must round fractions and retain NULL for zero denominator: %s", tc.tool, raw)
		}
	}
	rows, err := repository.Query(ctx, queryEntities["procurement.offers"].BaseSQL+" ORDER BY o.id")
	if err != nil || len(rows) != 2 || numericFloat(rows[0]["price_per_unit_cents"]) != 33.33 || rows[1]["price_per_unit_cents"] != nil {
		t.Fatalf("curated offer query fraction/zero handling: %#v, %v", rows, err)
	}
}

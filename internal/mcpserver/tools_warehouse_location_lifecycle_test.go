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

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestWarehouseLocationLifecycleAndAudit(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_WAREHOUSE_TEST_DATABASE_URL to a disposable _test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("dedicated _test database required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_location_lifecycle_test"
	if _, err = database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE; CREATE SCHEMA " + schema + "; SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE warehouse_schema_migrations(version TEXT PRIMARY KEY);
CREATE TABLE storage_zones(zone_id SERIAL PRIMARY KEY,code TEXT,barcode TEXT,name TEXT,type TEXT,description TEXT,parent_zone_id INT,capacity NUMERIC,is_active BOOLEAN DEFAULT true,location_kind TEXT DEFAULT 'area',process_role TEXT DEFAULT 'storage',operational_status TEXT DEFAULT 'available',is_storable BOOLEAN DEFAULT true,pick_sequence INT,capacity_mode TEXT DEFAULT 'item_count',max_weight_kg NUMERIC,max_volume_m3 NUMERIC,inventory_frequency_days INT,next_count_at TIMESTAMP,last_counted_at TIMESTAMP,profile_id BIGINT,updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE devices(zone_id INT,lifecycle_status TEXT,status TEXT);
CREATE TABLE cases(zone_id INT,home_zone_id INT);
CREATE TABLE product_locations(zone_id INT,quantity NUMERIC);
CREATE TABLE warehouse_tasks(from_zone_id INT,to_zone_id INT,status TEXT);
CREATE TABLE inventory_counts(zone_id INT,status TEXT);
CREATE TABLE audit_log(id BIGSERIAL PRIMARY KEY,user_id BIGINT,action TEXT,entity_type TEXT,entity_id TEXT,old_values JSONB,new_values JSONB,user_agent TEXT,timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE warehouse_product_mutation_receipts(id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL,operation VARCHAR(80) NOT NULL,key_hash CHAR(64) NOT NULL,request_hash CHAR(64) NOT NULL,response JSONB NOT NULL DEFAULT '{}'::jsonb,status_code INTEGER NOT NULL DEFAULT 200,created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,UNIQUE(user_id,operation,key_hash));
INSERT INTO storage_zones(code,barcode,name,type,description,inventory_frequency_days,last_counted_at,next_count_at,profile_id) VALUES('LIFE','LOC-LIFE','Lifecycle Fixture','shelf','private description',14,'2026-09-01','2026-09-15',1);
`)
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool, fn func(context.Context) (preparedMutation, error)) (preparedMutation, error) {
		var p preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { p, resultErr = fn(r.Context()) }))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, resultErr
	}
	prepare := func(in WarehouseLocationLifecycleInput, op string) preparedMutation {
		t.Helper()
		p, err := run(true, func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseLocationLifecycle(ctx, db, in, op)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "archive")
	if !p.Ready || p.Draft["confirmation_text_required"] != "ARCHIVE WAREHOUSE LOCATION 1" {
		t.Fatal(p)
	}
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1, ConfirmLifecycle: true}, "archive").Ready || prepare(WarehouseLocationLifecycleInput{ZoneID: 1, ExpectedUpdatedAt: "stale"}, "archive").Ready {
		t.Fatal("missing or stale version accepted")
	}
	for _, dep := range []struct{ insert, clear, key string }{
		{`INSERT INTO devices VALUES(1,'active','rented')`, `DELETE FROM devices`, "active_devices"},
		{`INSERT INTO cases VALUES(NULL,1)`, `DELETE FROM cases`, "home_cases"},
		{`INSERT INTO cases VALUES(1,NULL)`, `DELETE FROM cases`, "cases"},
		{`INSERT INTO product_locations VALUES(1,1),(1,-1)`, `DELETE FROM product_locations`, "stock_lines"},
		{`INSERT INTO warehouse_tasks VALUES(NULL,1,NULL)`, `DELETE FROM warehouse_tasks`, "open_tasks"},
		{`INSERT INTO inventory_counts VALUES(1,NULL)`, `DELETE FROM inventory_counts`, "open_counts"},
		{`INSERT INTO storage_zones(zone_id,code,barcode,name,type,parent_zone_id,is_active) VALUES(2,'P','P','Parent','rack',1,false),(3,'C','C','Child','shelf',2,true)`, `DELETE FROM storage_zones WHERE zone_id IN (2,3)`, "active_descendants"},
	} {
		exec(dep.insert)
		p = prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "archive")
		if p.Ready || !containsString(p.Missing, dep.key) {
			t.Fatal(dep.key, p)
		}
		exec(dep.clear)
	}
	exec(`UPDATE storage_zones SET operational_status='counting' WHERE zone_id=1`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "archive").Ready {
		t.Fatal("counting accepted")
	}
	exec(`UPDATE storage_zones SET is_active=false,operational_status='archived' WHERE zone_id=1`)
	p = prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore")
	if !p.Ready || p.Draft["operational_status"] != "blocked" {
		t.Fatal(p)
	}
	exec(`INSERT INTO storage_zones(zone_id,code,barcode,name,type,is_active) VALUES(2,'P','P','Parent','rack',false);UPDATE storage_zones SET parent_zone_id=2 WHERE zone_id=1`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore").Ready {
		t.Fatal("inactive parent accepted")
	}
	exec(`UPDATE storage_zones SET parent_zone_id=1 WHERE zone_id=1`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore").Ready {
		t.Fatal("cycle accepted")
	}
	exec(`UPDATE storage_zones SET parent_zone_id=NULL WHERE zone_id=1;UPDATE storage_zones SET barcode='LOC-LIFE' WHERE zone_id=2`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore").Ready {
		t.Fatal("duplicate scan accepted")
	}
	exec(`UPDATE storage_zones SET barcode='P',is_active=true,parent_zone_id=3 WHERE zone_id=2;INSERT INTO storage_zones(zone_id,code,barcode,name,type,parent_zone_id,is_active) VALUES(3,'C','C','Cycle','rack',2,true);UPDATE storage_zones SET parent_zone_id=2 WHERE zone_id=1`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore").Ready {
		t.Fatal("ancestor cycle accepted")
	}
	exec(`UPDATE storage_zones SET parent_zone_id=99 WHERE zone_id=2`)
	if prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore").Ready {
		t.Fatal("missing ancestor accepted")
	}
	exec(`UPDATE storage_zones SET parent_zone_id=NULL WHERE zone_id=1;DELETE FROM storage_zones WHERE zone_id=3`)
	exec(`DELETE FROM storage_zones WHERE zone_id=2`)
	if _, err := run(false, func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseLocationLifecycle(ctx, db, WarehouseLocationLifecycleInput{ZoneID: 1}, "restore")
	}); err == nil {
		t.Fatal("nonadmin accepted")
	}
	exec(`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values,user_agent) SELECT 11,'storage_zone.archive','storage_zone','1','{"name":"Lifecycle Fixture","operational_status":"maintenance","description":"secret description"}',jsonb_build_object('origin','MCP/AI','updated_at',to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'after',jsonb_build_object('name',name,'is_active',false,'operational_status','archived','description','secret description')),'private-agent' FROM storage_zones WHERE zone_id=1`)
	p = prepare(WarehouseLocationLifecycleInput{ZoneID: 1}, "restore")
	if !p.Ready || p.Draft["operational_status"] != "maintenance" {
		t.Fatal(p)
	}
	_, err = run(true, func(ctx context.Context) (preparedMutation, error) {
		result, _, _, err := warehouseLocationAuditHistory(ctx, db, WarehouseLocationAuditInput{ZoneID: 1})
		if err != nil {
			return preparedMutation{}, err
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "secret description") || strings.Contains(string(raw), "private-agent") || strings.Contains(string(raw), "old_values") {
			t.Fatal("audit privacy")
		}
		events := result.(map[string]any)["events"].([]map[string]any)
		if len(events) != 1 || events[0]["operational_status_before"] != "maintenance" || events[0]["result_version"] == nil {
			t.Fatal(result)
		}
		return preparedMutation{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"archive", "restore"} {
		if requiredMutationScope("warehouse.locations."+op) != "cores:warehouse:archive" || !isMutationTool("warehouse.locations."+op) {
			t.Fatal("scope/registration")
		}
	}
	schemaResult := renderWritableEntitySchema(writableEntitySchemas()["warehouse.locations"])
	if len(schemaResult["lifecycle_fields"].([]map[string]any)) != 4 {
		t.Fatal(schemaResult)
	}
}

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

func TestWarehouseDeviceSchemaAndScopes(t *testing.T) {
	schema := renderWritableEntitySchema(writableEntitySchemas()["warehouse.devices"])
	for _, key := range []string{"fields", "update_fields", "lifecycle_fields", "revert_fields"} {
		if len(schema[key].([]map[string]any)) == 0 {
			t.Fatal(key)
		}
	}
	for _, tc := range []struct{ action, scope string }{{"create", "create"}, {"update", "update"}, {"revert_update", "update"}, {"archive", "archive"}, {"restore", "archive"}} {
		if requiredMutationScope("warehouse.devices."+tc.action) != "cores:warehouse:"+tc.scope {
			t.Fatal(tc)
		}
	}
}
func TestWarehouseDevicePreparationAndRedactedAudit(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable _test database required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("dedicated _test database required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := database.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("DROP SCHEMA IF EXISTS mcp_device_workflows_test CASCADE; CREATE SCHEMA mcp_device_workflows_test; SET search_path TO mcp_device_workflows_test")
	defer database.Exec("DROP SCHEMA mcp_device_workflows_test CASCADE")
	exec(warehouseDeviceFixtureSQL)
	exec(`INSERT INTO devices(deviceid,productid,serialnumber,barcode,qr_code,status,condition_status,notes,usage_hours,updated_at) VALUES('DEV-A',1,'Serial-A','BAR-A','QR-A','location_unknown','defective','secret notes',12.34,'2026-09-30 10:15:00.123456'),('DEV-B',1,'Serial-B','BAR-B','QR-B','location_unknown','available',NULL,0,CURRENT_TIMESTAMP); UPDATE devices SET lifecycle_status='archived' WHERE deviceid='DEV-B'`)
	db := store.New(database, 5*time.Second, 200)
	run := func(admin bool, actor string, fn func(context.Context)) {
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: actor, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		h := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { fn(r.Context()) }))
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	prepare := func(fn func(context.Context) (preparedMutation, error)) preparedMutation {
		t.Helper()
		var p preparedMutation
		run(true, "11", func(ctx context.Context) {
			var err error
			p, err = fn(ctx)
			if err != nil {
				t.Fatal(err)
			}
		})
		return p
	}
	create := func(in WarehouseDeviceCreateInput) preparedMutation {
		return prepare(func(ctx context.Context) (preparedMutation, error) { return prepareWarehouseDeviceCreate(ctx, db, in) })
	}
	update := func(in WarehouseDeviceUpdateInput) preparedMutation {
		return prepare(func(ctx context.Context) (preparedMutation, error) { return prepareWarehouseDeviceUpdate(ctx, db, in) })
	}
	life := func(op string) preparedMutation {
		return prepare(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseDeviceLifecycle(ctx, db, WarehouseDeviceLifecycleInput{DeviceID: "DEV-A"}, op)
		})
	}
	if p := create(WarehouseDeviceCreateInput{ProductID: 1}); !p.Ready || p.Draft["condition_rating"] != float64(5) || p.Draft["barcode"] != (*string)(nil) {
		t.Fatalf("defaults: %#v", p)
	}
	for _, tc := range []struct {
		input WarehouseDeviceCreateInput
		field string
	}{{WarehouseDeviceCreateInput{ProductID: 3}, "product_id"}, {WarehouseDeviceCreateInput{ProductID: 4}, "product_id"}, {WarehouseDeviceCreateInput{ProductID: 999}, "product_id"}, {WarehouseDeviceCreateInput{ProductID: 1, SerialNumber: " serial-b "}, "duplicate_serial"}, {WarehouseDeviceCreateInput{ProductID: 1, Barcode: "bar-b"}, "identifier_conflict"}, {WarehouseDeviceCreateInput{ProductID: 1, PurchaseDate: "2026-02-30"}, "purchase_date"}, {WarehouseDeviceCreateInput{ProductID: 1, LastMaintenance: "2026-01-02", NextMaintenance: "2026-01-01"}, "next_maintenance"}} {
		p := create(tc.input)
		if p.Ready || !containsString(p.Missing, tc.field) {
			t.Fatalf("missing %s: %#v", tc.field, p)
		}
	}
	for _, v := range []float64{-1, 1.234, 100000000} {
		p := create(WarehouseDeviceCreateInput{ProductID: 1, UsageHours: &v})
		if p.Ready || !containsString(p.Missing, "usage_hours") {
			t.Fatal(p)
		}
	}
	zone := int64(1)
	if !create(WarehouseDeviceCreateInput{ProductID: 1, ZoneID: &zone}).Ready {
		t.Fatal("empty capacity")
	}
	exec(`INSERT INTO cases VALUES(1,1)`)
	if p := create(WarehouseDeviceCreateInput{ProductID: 1, ZoneID: &zone}); p.Ready {
		t.Fatal("full location accepted")
	}
	exec(`DELETE FROM cases`)
	notes := "new secret notes"
	p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", Notes: &notes})
	if !p.Ready || len(p.Diff) != 1 || p.Draft["usage_hours"] != 12.34 || p.Draft["expected_updated_at"] != "2026-09-30T10:15:00.123456Z" {
		t.Fatalf("partial update lost fields: %#v", p)
	}
	if p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", Notes: &notes, ConfirmUpdate: true}); p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatal("missing version accepted")
	}
	if p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", Notes: &notes, ExpectedUpdatedAt: "stale"}); p.Ready {
		t.Fatal("stale version accepted")
	}
	if p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", ClearFields: []string{"notes"}}); !p.Ready || p.Draft["notes"] != (*string)(nil) {
		t.Fatal("clear failed")
	}
	if p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", Notes: &notes, ClearFields: []string{"notes"}}); p.Ready {
		t.Fatal("clear/set conflict accepted")
	}
	if p := update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", ClearFields: []string{"barcode"}}); p.Ready {
		t.Fatal("scan clear accepted")
	}
	exec(`INSERT INTO jobs VALUES(3,NULL,NULL);INSERT INTO job_devices VALUES('DEV-A',3,'pending')`)
	if life("archive").Ready {
		t.Fatal("unknown job status ignored")
	}
	exec(`DELETE FROM job_devices`)
	exec(`INSERT INTO warehouse_tasks VALUES('DEV-A','open')`)
	if life("archive").Ready {
		t.Fatal("task dependency ignored")
	}
	serial := "New Serial"
	if update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", SerialNumber: &serial}).Ready {
		t.Fatal("identity dependency ignored")
	}
	if !update(WarehouseDeviceUpdateInput{DeviceID: "DEV-A", Notes: &notes}).Ready {
		t.Fatal("metadata should remain editable")
	}
	exec(`DELETE FROM warehouse_tasks`)
	if !life("archive").Ready {
		t.Fatal("archive unavailable")
	}
	exec(`UPDATE devices SET lifecycle_status='archived',zone_id=1,status='in_storage' WHERE deviceid='DEV-A'`)
	if !life("restore").Ready {
		t.Fatal("restore unavailable")
	}
	exec(`INSERT INTO inventory_identifiers VALUES('device','DEV-A','alias','CustomAlias',false),('case','other','canonical','customalias',true)`)
	if life("restore").Ready {
		t.Fatal("alias conflict ignored")
	}
	exec(`DELETE FROM inventory_identifiers`)
	exec(`INSERT INTO cases VALUES(1,1)`)
	if life("restore").Ready {
		t.Fatal("restore capacity ignored")
	}
	exec(`DELETE FROM cases; UPDATE devices SET lifecycle_status='active',zone_id=NULL,status='location_unknown' WHERE deviceid='DEV-A'`)
	// Eligible inverse update uses only the actual actor and unchanged latest audit/version.
	exec(`INSERT INTO audit_log(user_id,action,entity_type,entity_id,old_values,new_values) VALUES(11,'device.update','device','DEV-A','{"product_id":1,"serial_number":"Serial-A","barcode":"BAR-A","qr_code":"QR-A","condition_rating":5,"usage_hours":12.34,"purchase_date":null,"last_maintenance":null,"next_maintenance":null,"notes":"previous secret notes"}','{"origin":"MCP/AI","updated_at":"2026-09-30T10:15:00.123456Z","after":{"product_id":1,"barcode":"BAR-A","qr_code":"QR-A","notes":"secret notes"}}')`)
	revert := func() preparedMutation {
		return prepare(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseDeviceRevert(ctx, db, WarehouseDeviceRevertInput{DeviceID: "DEV-A", AuditID: 1})
		})
	}
	if p := revert(); !p.Ready || len(p.Diff) != 1 || p.Diff["notes"] == nil {
		t.Fatalf("revert %#v", p)
	}
	run(true, "12", func(ctx context.Context) {
		p, err := prepareWarehouseDeviceRevert(ctx, db, WarehouseDeviceRevertInput{DeviceID: "DEV-A", AuditID: 1})
		if err != nil || p.Ready {
			t.Fatal("foreign actor accepted")
		}
	})
	run(true, "11", func(ctx context.Context) {
		data, _, _, err := warehouseDeviceAuditHistory(ctx, db, WarehouseDeviceAuditInput{DeviceID: "DEV-A"})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(data)
		if strings.Contains(string(encoded), "secret notes") || strings.Contains(string(encoded), "old_values") || strings.Contains(string(encoded), "user_agent") {
			t.Fatal("audit leaks private fields")
		}
		events := data.(map[string]any)["events"].([]map[string]any)
		if len(events) != 1 || events[0]["can_prepare_revert"] != true {
			t.Fatal(data)
		}
	})
	exec(`UPDATE devices SET updated_at=updated_at+interval '1 microsecond' WHERE deviceid='DEV-A'`)
	if revert().Ready {
		t.Fatal("later write accepted")
	}
	exec(`UPDATE devices SET updated_at='2026-09-30 10:15:00.123456' WHERE deviceid='DEV-A'; INSERT INTO audit_log(user_id,action,entity_type,entity_id) VALUES(11,'device.status','device','DEV-A')`)
	if revert().Ready {
		t.Fatal("later audit accepted")
	}
	run(false, "11", func(ctx context.Context) {
		_, err := prepareWarehouseDeviceCreate(ctx, db, WarehouseDeviceCreateInput{ProductID: 1})
		if err == nil {
			t.Fatal("nonadmin preview accepted")
		}
		_, _, _, err = warehouseDeviceAuditHistory(ctx, db, WarehouseDeviceAuditInput{DeviceID: "DEV-A"})
		if err == nil {
			t.Fatal("nonadmin history accepted")
		}
	})
}

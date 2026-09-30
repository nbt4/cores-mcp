package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseDeviceCreateInput struct {
	MutationControl
	ProductID       int64    `json:"product_id,omitempty" jsonschema:"Existing active individually tracked product ID."`
	SerialNumber    string   `json:"serial_number,omitempty" jsonschema:"Optional serial number, maximum 255 characters, unique across active and archived devices."`
	Barcode         string   `json:"barcode,omitempty" jsonschema:"Optional reserved scan code, maximum 255 characters; omitted generates the device ID."`
	QRCode          string   `json:"qr_code,omitempty" jsonschema:"Optional reserved QR value, maximum 255 characters; omitted generates WH: plus device ID."`
	ZoneID          *int64   `json:"zone_id,omitempty" jsonschema:"Optional available storable location with capacity. Physical state is derived, never set directly."`
	ConditionStatus string   `json:"condition_status,omitempty" jsonschema:"Initial condition: available (default), blocked, defective, maintenance or retired."`
	ConditionRating *float64 `json:"condition_rating,omitempty" jsonschema:"Condition score 0-5, at most one decimal; defaults to 5."`
	UsageHours      *float64 `json:"usage_hours,omitempty" jsonschema:"Nonnegative usage hours with two decimals, maximum 99999999.99; defaults to 0."`
	PurchaseDate    string   `json:"purchase_date,omitempty" jsonschema:"Optional valid YYYY-MM-DD date."`
	LastMaintenance string   `json:"last_maintenance,omitempty" jsonschema:"Optional valid YYYY-MM-DD date; does not complete maintenance orders."`
	NextMaintenance string   `json:"next_maintenance,omitempty" jsonschema:"Optional valid YYYY-MM-DD date, not before last maintenance; does not alter maintenance plans."`
	Notes           string   `json:"notes,omitempty" jsonschema:"Optional notes, maximum 4000 characters."`
	ConfirmCreation bool     `json:"confirm_creation,omitempty" jsonschema:"Set only after showing the entire device draft and obtaining confirmation."`
}
type WarehouseDeviceUpdateInput struct {
	MutationControl
	DeviceID          string   `json:"device_id,omitempty" jsonschema:"Exact active device ID; immutable."`
	ProductID         *int64   `json:"product_id,omitempty" jsonschema:"Replacement active serialized product; reassociation is blocked by device history and active dependencies."`
	SerialNumber      *string  `json:"serial_number,omitempty" jsonschema:"Replacement unique serial, maximum 255 characters; empty clears. Active dependencies block identity edits."`
	Barcode           *string  `json:"barcode,omitempty" jsonschema:"Replacement reserved barcode, maximum 255 characters; cannot be cleared. Review physical labels after changing."`
	QRCode            *string  `json:"qr_code,omitempty" jsonschema:"Replacement reserved QR value, maximum 255 characters; cannot be cleared. Review physical labels after changing."`
	ConditionRating   *float64 `json:"condition_rating,omitempty" jsonschema:"Replacement condition score 0-5, at most one decimal."`
	UsageHours        *float64 `json:"usage_hours,omitempty" jsonschema:"Replacement nonnegative hours with at most two decimals, maximum 99999999.99."`
	PurchaseDate      *string  `json:"purchase_date,omitempty" jsonschema:"Replacement valid YYYY-MM-DD date; empty clears."`
	LastMaintenance   *string  `json:"last_maintenance,omitempty" jsonschema:"Replacement valid YYYY-MM-DD date; empty clears. No order completion."`
	NextMaintenance   *string  `json:"next_maintenance,omitempty" jsonschema:"Replacement valid YYYY-MM-DD date; empty clears. No maintenance plan change."`
	Notes             *string  `json:"notes,omitempty" jsonschema:"Replacement notes, maximum 4000 characters; empty clears."`
	ClearFields       []string `json:"clear_fields,omitempty" jsonschema:"Explicitly clear serial_number, purchase_date, last_maintenance, next_maintenance or notes; do not also supply that field."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact full-device version from prepare_update."`
	ConfirmUpdate     bool     `json:"confirm_update,omitempty" jsonschema:"Set only after showing the complete diff and receiving confirmation."`
}
type WarehouseDeviceLifecycleInput struct {
	MutationControl
	DeviceID          string `json:"device_id,omitempty" jsonschema:"Exact device ID."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact unchanged version from lifecycle preview."`
	ConfirmLifecycle  bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after showing all dependencies and lifecycle diff and receiving confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact device-bound phrase from preview."`
}
type WarehouseDeviceRevertInput struct {
	MutationControl
	DeviceID          string `json:"device_id,omitempty" jsonschema:"Exact active device ID."`
	AuditID           int64  `json:"audit_id,omitempty" jsonschema:"Your latest device.update audit ID. Revert is blocked after any newer device write or audit."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact unchanged version from revert preview."`
	ConfirmRevert     bool   `json:"confirm_revert,omitempty" jsonschema:"Set only after showing the full inverse diff and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact audit/device-bound phrase from preview."`
}
type WarehouseDeviceAuditInput struct {
	DeviceID string `json:"device_id" jsonschema:"Exact device ID, including archived devices."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum redacted events, capped at 100."`
}
type warehouseDeviceDraft struct {
	ProductID       int64   `json:"product_id"`
	SerialNumber    *string `json:"serial_number"`
	Barcode         *string `json:"barcode"`
	QRCode          *string `json:"qr_code"`
	ConditionRating float64 `json:"condition_rating"`
	UsageHours      float64 `json:"usage_hours"`
	PurchaseDate    *string `json:"purchase_date"`
	LastMaintenance *string `json:"last_maintenance"`
	NextMaintenance *string `json:"next_maintenance"`
	Notes           *string `json:"notes"`
}

const warehouseDeviceDependenciesSQL = `SELECT
 (SELECT count(*) FROM job_devices jd JOIN jobs j ON j.jobid=jd.jobid JOIN status s ON s.statusid=j.statusid WHERE jd.deviceid=$1 AND ((j.deleted_at IS NULL AND NOT warehouse_job_status_is_closed(s.status)) OR jd.pack_status IN ('packed','issued'))) AS jobs,
 (SELECT count(*) FROM job_position_devices pd JOIN job_positions p ON p.position_id=pd.position_id JOIN jobs j ON j.jobid=p.job_id JOIN status s ON s.statusid=j.statusid WHERE pd.device_id=$1 AND j.deleted_at IS NULL AND NOT warehouse_job_status_is_closed(s.status)) AS picklists,
 (SELECT count(*) FROM job_package_reservations WHERE device_id=$1 AND reservation_status<>'released') AS reservations,
 (SELECT count(*) FROM devicescases WHERE deviceid=$1)+(SELECT count(*) FROM devices WHERE deviceid=$1 AND current_case_id IS NOT NULL) AS cases,
 (SELECT count(*) FROM device_components WHERE device_id=$1 OR component_device_id=$1) AS components,
 (SELECT count(*) FROM warehouse_tasks WHERE device_id=$1 AND lower(status) NOT IN ('completed','done','cancelled','canceled','closed')) AS tasks,
 (SELECT count(*) FROM maintenance_orders WHERE device_id=$1 AND status NOT IN ('completed','cancelled')) AS maintenance_orders,
 (SELECT count(*) FROM maintenance_plans WHERE device_id=$1 AND is_active) AS maintenance_plans,
 (SELECT count(*) FROM defect_reports WHERE device_id=$1 AND lower(COALESCE(status,'')) NOT IN ('resolved','closed','done','completed')) AS defects
`

const warehouseDeviceFieldsSQL = `SELECT deviceid AS device_id,lifecycle_status,status AS physical_status,condition_status,zone_id,current_case_id,archived_by_product,
 to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,
 jsonb_build_object('product_id',productid,'serial_number',serialnumber,'barcode',barcode,'qr_code',qr_code,
 'condition_rating',COALESCE(condition_rating,5),'usage_hours',COALESCE(usage_hours,0),'purchase_date',to_char(purchasedate,'YYYY-MM-DD'),
 'last_maintenance',to_char(lastmaintenance,'YYYY-MM-DD'),'next_maintenance',to_char(nextmaintenance,'YYYY-MM-DD'),'notes',notes) AS fields
 FROM devices WHERE deviceid=$1`

func deviceSources(id string) []Source {
	if id == "" {
		return []Source{{Service: "warehousecore", Entity: "device_draft"}}
	}
	return []Source{{Service: "warehousecore", Entity: "device", ID: id}}
}
func deviceDraftMap(d warehouseDeviceDraft) map[string]any {
	return map[string]any{"product_id": d.ProductID, "serial_number": d.SerialNumber, "barcode": d.Barcode, "qr_code": d.QRCode, "condition_rating": d.ConditionRating, "usage_hours": d.UsageHours, "purchase_date": d.PurchaseDate, "last_maintenance": d.LastMaintenance, "next_maintenance": d.NextMaintenance, "notes": d.Notes}
}
func decodeDeviceFields(raw any) (warehouseDeviceDraft, error) {
	var fields warehouseDeviceDraft
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fields, err
	}
	err = json.Unmarshal(encoded, &fields)
	return fields, err
}
func exactDeviceID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && len([]rune(id)) <= 50
}

func registerWarehouseDeviceTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.devices.prepare_create", "Prepare serialized device", "Preview one device, all metadata, reserved identities, active serialized product, initial condition and optional storage capacity. No labels or files are generated.", func(ctx context.Context, in WarehouseDeviceCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceCreate(ctx, db, in)
		return p.response("draft"), deviceSources(""), p.Warnings, err
	})
	addCreateTool(server, "warehouse.devices.create", "Create serialized device", "Create one device after full preview and confirmation. WarehouseCore commits device, scan identifiers, audit and durable replay atomically.", func(ctx context.Context, in WarehouseDeviceCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceCreate(ctx, db, in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), deviceSources(""), p.Warnings, err
		}
		if !in.ConfirmCreation {
			return p.response("confirmation_required"), deviceSources(""), p.Warnings, nil
		}
		var result map[string]any
		if err = api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/devices", http.MethodPost, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "device": result}, deviceSources(nullableText(result["device_id"])), p.Warnings, nil
	})
	addWritePreparationTool(server, "warehouse.devices.prepare_update", "Prepare device metadata update", "Load every editable metadata field and exact version; show the full diff and references. Movements and condition/status changes use their dedicated workflows.", func(ctx context.Context, in WarehouseDeviceUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceUpdate(ctx, db, in)
		return p.response("draft"), deviceSources(in.DeviceID), p.Warnings, err
	})
	addUpdateTool(server, "warehouse.devices.update", "Update device metadata", "Update one active device after exact full-device version, dependency review, complete diff and confirmation; IDs and physical/condition states are preserved. Audit and replay are atomic.", func(ctx context.Context, in WarehouseDeviceUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceUpdate(ctx, db, in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), deviceSources(in.DeviceID), p.Warnings, err
		}
		if !in.ConfirmUpdate {
			return p.response("confirmation_required"), deviceSources(in.DeviceID), p.Warnings, nil
		}
		var result map[string]any
		if err = api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/devices/"+url.PathEscape(in.DeviceID), http.MethodPut, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "device": result, "diff": p.Diff}, deviceSources(in.DeviceID), p.Warnings, nil
	})
	for _, operation := range []string{"archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "warehouse.devices.prepare_"+op, "Prepare device "+op, "Preview exact version, lifecycle diff, active jobs, reservations, cases, components, tasks, defects and maintenance; restoring also validates product, scan codes and storage capacity.", func(ctx context.Context, in WarehouseDeviceLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseDeviceLifecycle(ctx, db, in, op)
			return p.response("draft"), deviceSources(in.DeviceID), p.Warnings, err
		})
		addUpdateTool(server, "warehouse.devices."+op, "Device "+op, "Archive or restore one device with dedicated archive scope, admin, exact version and device-bound confirmation. History and condition remain; all scan identifiers follow lifecycle. Core rechecks dependencies atomically.", func(ctx context.Context, in WarehouseDeviceLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseDeviceLifecycle(ctx, db, in, op)
			if err != nil || !p.Ready {
				return p.response("needs_input"), deviceSources(in.DeviceID), p.Warnings, err
			}
			if !in.ConfirmLifecycle {
				return p.response("confirmation_required"), deviceSources(in.DeviceID), p.Warnings, nil
			}
			phrase := strings.ToUpper(op) + " WAREHOUSE DEVICE " + in.DeviceID
			if in.ConfirmationText != phrase {
				return p.response("elevated_confirmation_required"), deviceSources(in.DeviceID), p.Warnings, nil
			}
			method, path := http.MethodDelete, "/api/v1/admin/devices/"+url.PathEscape(in.DeviceID)
			if op == "restore" {
				method, path = http.MethodPut, path+"/restore"
			}
			var result map[string]any
			if err = api.doJSON(ctx, cfg.WarehouseURL, path, method, map[string]any{"expected_updated_at": in.ExpectedUpdatedAt, "confirm_lifecycle": true, "confirmation_text": phrase}, &result); err != nil {
				return nil, nil, nil, err
			}
			return map[string]any{"operation_status": op + "d", "device": result, "diff": p.Diff}, deviceSources(in.DeviceID), p.Warnings, nil
		})
	}
	addWritePreparationTool(server, "warehouse.devices.prepare_revert_update", "Prepare inverse device edit", "Preview only your latest unchanged MCP device field update, identified by its audit ID. Any later device write or audit blocks revert; all reference and identity rules are checked again.", func(ctx context.Context, in WarehouseDeviceRevertInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceRevert(ctx, db, in)
		return p.response("draft"), deviceSources(in.DeviceID), p.Warnings, err
	})
	addUpdateTool(server, "warehouse.devices.revert_update", "Revert your last device edit", "Apply a complete inverse field diff with admin/update scope, exact version and audit/device-bound confirmation. Only your latest unchanged MCP device.update is eligible. Revert has its own retained audit and receipt.", func(ctx context.Context, in WarehouseDeviceRevertInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseDeviceRevert(ctx, db, in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), deviceSources(in.DeviceID), p.Warnings, err
		}
		if !in.ConfirmRevert {
			return p.response("confirmation_required"), deviceSources(in.DeviceID), p.Warnings, nil
		}
		phrase := fmt.Sprintf("REVERT WAREHOUSE DEVICE %s UPDATE %d", in.DeviceID, in.AuditID)
		if in.ConfirmationText != phrase {
			return p.response("elevated_confirmation_required"), deviceSources(in.DeviceID), p.Warnings, nil
		}
		var result map[string]any
		if err = api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/devices/"+url.PathEscape(in.DeviceID)+"/revert-update", http.MethodPost, map[string]any{"audit_id": in.AuditID, "expected_updated_at": in.ExpectedUpdatedAt, "confirm_revert": true, "confirmation_text": phrase}, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "reverted", "device": result, "diff": p.Diff}, deviceSources(in.DeviceID), p.Warnings, nil
	})
}

func loadWarehouseDevicePreview(ctx context.Context, db *store.Store, p *preparedMutation, id, expected string, confirmed bool) (warehouseDeviceDraft, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return warehouseDeviceDraft{}, err
	}
	if !exactDeviceID(id) {
		p.require("device_id", "Exakte Geräte-ID erforderlich.", nil)
		return warehouseDeviceDraft{}, nil
	}
	rows, err := db.Query(ctx, warehouseDeviceFieldsSQL, id)
	if err != nil {
		return warehouseDeviceDraft{}, err
	}
	if len(rows) != 1 {
		p.require("device_id", "Gerät wurde nicht gefunden.", nil)
		return warehouseDeviceDraft{}, nil
	}
	p.Current = rows[0]
	version := nullableText(p.Current["updated_at"])
	if confirmed && expected == "" || expected != "" && expected != version {
		p.require("expected_updated_at", "Neue Vorschau mit exakter unveränderter Geräteversion erforderlich.", version)
	}
	return decodeDeviceFields(p.Current["fields"])
}
func prepareWarehouseDeviceCreate(ctx context.Context, db *store.Store, in WarehouseDeviceCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	d := warehouseDeviceDraft{ProductID: in.ProductID, SerialNumber: &in.SerialNumber, Barcode: &in.Barcode, QRCode: &in.QRCode, ConditionRating: 5, PurchaseDate: &in.PurchaseDate, LastMaintenance: &in.LastMaintenance, NextMaintenance: &in.NextMaintenance, Notes: &in.Notes}
	if in.ConditionRating != nil {
		d.ConditionRating = *in.ConditionRating
	}
	if in.UsageHours != nil {
		d.UsageHours = *in.UsageHours
	}
	err := validateWarehouseDeviceDraft(ctx, db, &p, &d, "", true)
	condition := strings.TrimSpace(in.ConditionStatus)
	if condition == "" {
		condition = "available"
	}
	if !map[string]bool{"available": true, "blocked": true, "defective": true, "maintenance": true, "retired": true}[condition] {
		p.require("condition_status", "Gültiger anfänglicher Betriebszustand erforderlich.", nil)
	}
	p.Draft = deviceDraftMap(d)
	p.Draft["zone_id"], p.Draft["condition_status"] = in.ZoneID, condition
	if in.ZoneID != nil {
		if *in.ZoneID <= 0 || *in.ZoneID > math.MaxInt32 {
			p.require("zone_id", "Gültiger Lagerplatz erforderlich.", nil)
		} else if zoneErr := previewDeviceDestination(ctx, db, &p, *in.ZoneID); zoneErr != nil {
			return p, zoneErr
		}
	}
	p.finish()
	return p, err
}
func prepareWarehouseDeviceUpdate(ctx context.Context, db *store.Store, in WarehouseDeviceUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	current, err := loadWarehouseDevicePreview(ctx, db, &p, in.DeviceID, in.ExpectedUpdatedAt, in.ConfirmUpdate)
	if err != nil || p.Current == nil {
		p.finish()
		return p, err
	}
	if p.Current["lifecycle_status"] != "active" {
		p.require("lifecycle_status", "Archiviertes Gerät zuerst wiederherstellen.", nil)
	}
	d := current
	if in.ProductID != nil {
		d.ProductID = *in.ProductID
	}
	if in.SerialNumber != nil {
		d.SerialNumber = in.SerialNumber
	}
	if in.Barcode != nil {
		d.Barcode = in.Barcode
	}
	if in.QRCode != nil {
		d.QRCode = in.QRCode
	}
	if in.ConditionRating != nil {
		d.ConditionRating = *in.ConditionRating
	}
	if in.UsageHours != nil {
		d.UsageHours = *in.UsageHours
	}
	if in.PurchaseDate != nil {
		d.PurchaseDate = in.PurchaseDate
	}
	if in.LastMaintenance != nil {
		d.LastMaintenance = in.LastMaintenance
	}
	if in.NextMaintenance != nil {
		d.NextMaintenance = in.NextMaintenance
	}
	if in.Notes != nil {
		d.Notes = in.Notes
	}
	pointers := map[string]**string{"serial_number": &d.SerialNumber, "purchase_date": &d.PurchaseDate, "last_maintenance": &d.LastMaintenance, "next_maintenance": &d.NextMaintenance, "notes": &d.Notes}
	supplied := map[string]*string{"serial_number": in.SerialNumber, "purchase_date": in.PurchaseDate, "last_maintenance": in.LastMaintenance, "next_maintenance": in.NextMaintenance, "notes": in.Notes}
	for _, field := range in.ClearFields {
		ptr, ok := pointers[field]
		if !ok {
			p.require("clear_fields", "Nur Seriennummer, Datumsfelder und Notizen sind leerbar.", nil)
		} else {
			if supplied[field] != nil {
				p.require(field, "Leeren oder Setzen wählen, nicht beides.", nil)
			}
			*ptr = nil
		}
	}
	if err = validateWarehouseDeviceDraft(ctx, db, &p, &d, in.DeviceID, false); err != nil {
		return p, err
	}
	if err = previewDeviceEditDependencies(ctx, db, &p, in.DeviceID, current, d); err != nil {
		return p, err
	}
	deviceDiff(&p, current, d)
	p.Draft = deviceDraftMap(d)
	p.Draft["expected_updated_at"] = p.Current["updated_at"]
	p.finish()
	return p, nil
}
func deviceDiff(p *preparedMutation, before, after warehouseDeviceDraft) {
	p.Diff = map[string]map[string]any{}
	a, b := deviceDraftMap(before), deviceDraftMap(after)
	for field, value := range b {
		if !reflect.DeepEqual(a[field], value) {
			p.Diff[field] = map[string]any{"before": a[field], "after": value}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Gerätefelder sollen geändert werden?", nil)
	}
}
func previewDeviceEditDependencies(ctx context.Context, db *store.Store, p *preparedMutation, id string, before, after warehouseDeviceDraft) error {
	counts, err := db.Query(ctx, warehouseDeviceDependenciesSQL, id)
	if err != nil {
		return err
	}
	p.RelatedRecords = append(p.RelatedRecords, counts...)
	identityChanged := before.ProductID != after.ProductID || !reflect.DeepEqual(before.SerialNumber, after.SerialNumber) || !reflect.DeepEqual(before.Barcode, after.Barcode) || !reflect.DeepEqual(before.QRCode, after.QRCode)
	if identityChanged && (devicePreviewHasDependencies(counts) || p.Current["physical_status"] == "on_job" || p.Current["physical_status"] == "return_pending") {
		p.require("active_dependencies", "Aktive Abhängigkeiten sperren Änderungen der Geräteidentität.", counts)
	}
	if before.ProductID != after.ProductID {
		rows, err := db.Query(ctx, `SELECT EXISTS(SELECT 1 FROM job_devices WHERE deviceid=$1) OR EXISTS(SELECT 1 FROM device_movements WHERE device_id=$1) OR EXISTS(SELECT 1 FROM maintenance_orders WHERE device_id=$1) OR EXISTS(SELECT 1 FROM maintenance_plans WHERE device_id=$1) OR EXISTS(SELECT 1 FROM defect_reports WHERE device_id=$1) OR EXISTS(SELECT 1 FROM job_package_reservations WHERE device_id=$1) OR EXISTS(SELECT 1 FROM job_position_devices WHERE device_id=$1) AS has_history`, id)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0]["has_history"] == true {
			p.require("device_history", "Gerätehistorie sperrt einen Produktwechsel.", nil)
		}
	}
	return nil
}
func devicePreviewHasDependencies(rows []map[string]any) bool {
	for _, row := range rows {
		for _, value := range row {
			if numericID(value) > 0 {
				return true
			}
		}
	}
	return false
}
func prepareWarehouseDeviceLifecycle(ctx context.Context, db *store.Store, in WarehouseDeviceLifecycleInput, operation string) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	current, err := loadWarehouseDevicePreview(ctx, db, &p, in.DeviceID, in.ExpectedUpdatedAt, in.ConfirmLifecycle)
	if err != nil || p.Current == nil {
		p.finish()
		return p, err
	}
	from, to := "active", "archived"
	if operation == "restore" {
		from, to = to, from
	} else if operation != "archive" {
		return p, fmt.Errorf("unsupported device lifecycle action")
	}
	if p.Current["lifecycle_status"] != from {
		p.require("lifecycle_status", "Aktueller Archivstatus erlaubt diesen Wechsel nicht.", nil)
	}
	counts, err := db.Query(ctx, warehouseDeviceDependenciesSQL, in.DeviceID)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = counts
	if devicePreviewHasDependencies(counts) || p.Current["physical_status"] == "on_job" || p.Current["physical_status"] == "return_pending" {
		p.require("active_dependencies", "Aktive Geräteabhängigkeiten zuerst auflösen.", counts)
	}
	if operation == "restore" {
		aliases, aliasErr := db.Query(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_identifiers own WHERE own.entity_type='device' AND own.entity_key=$1 AND (EXISTS(SELECT 1 FROM inventory_identifiers other WHERE NOT(other.entity_type='device' AND other.entity_key=$1) AND lower(trim(other.code))=lower(trim(own.code))) OR EXISTS(SELECT 1 FROM devices d WHERE d.deviceid<>$1 AND (lower(trim(d.deviceid))=lower(trim(own.code)) OR lower(trim(d.barcode))=lower(trim(own.code)) OR lower(trim(d.qr_code))=lower(trim(own.code)))))) AS conflict`, in.DeviceID)
		if aliasErr != nil {
			return p, aliasErr
		}
		if len(aliases) != 1 || aliases[0]["conflict"] == true {
			p.require("identifier_conflict", "Eine reservierte Gerätekennung kollidiert mit anderem Bestand.", nil)
		}
		if err = validateWarehouseDeviceDraft(ctx, db, &p, &current, in.DeviceID, false); err != nil {
			return p, err
		}
		if p.Current["zone_id"] != nil {
			if err = previewDeviceDestination(ctx, db, &p, numericID(p.Current["zone_id"])); err != nil {
				return p, err
			}
		} else if p.Current["physical_status"] == "in_storage" {
			p.require("zone_id", "Gelagertes Gerät benötigt einen gültigen Lagerplatz.", nil)
		}
	}
	p.Draft = map[string]any{"device_id": in.DeviceID, "expected_updated_at": p.Current["updated_at"], "confirmation_text_required": strings.ToUpper(operation) + " WAREHOUSE DEVICE " + in.DeviceID}
	p.Diff = map[string]map[string]any{"lifecycle_status": {"before": from, "after": to}}
	p.Warnings = append(p.Warnings, "Archive retains device fields, history, physical state and condition; all scan identifiers are disabled. Restore rechecks product, identifiers, dependencies and capacity without changing condition.")
	p.finish()
	return p, nil
}
func prepareWarehouseDeviceRevert(ctx context.Context, db *store.Store, in WarehouseDeviceRevertInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	current, err := loadWarehouseDevicePreview(ctx, db, &p, in.DeviceID, in.ExpectedUpdatedAt, in.ConfirmRevert)
	if err != nil || p.Current == nil {
		p.finish()
		return p, err
	}
	if p.Current["lifecycle_status"] != "active" {
		p.require("lifecycle_status", "Nur aktive Geräte erlauben Feld-Revert.", nil)
	}
	if in.AuditID <= 0 {
		p.require("audit_id", "Audit-ID der eigenen letzten MCP-Feldänderung erforderlich.", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT id,user_id,action,old_values,new_values->>'origin' AS origin,new_values->>'updated_at' AS result_version,(SELECT max(id) FROM audit_log WHERE entity_type='device' AND entity_id=$2) AS latest_id FROM audit_log WHERE id=$1 AND entity_type='device' AND entity_id=$2`, in.AuditID, in.DeviceID)
	if err != nil {
		return p, err
	}
	actor := auth.TokenInfoFromContext(ctx)
	if len(rows) != 1 || rows[0]["action"] != "device.update" || rows[0]["origin"] != "MCP/AI" || fmt.Sprint(rows[0]["user_id"]) != actor.UserID || numericID(rows[0]["latest_id"]) != in.AuditID || rows[0]["result_version"] != p.Current["updated_at"] {
		p.require("audit_id", "Nur die eigene letzte unveränderte MCP-Geräteänderung kann rückgängig gemacht werden.", nil)
		p.finish()
		return p, nil
	}
	previous, err := decodeDeviceFields(rows[0]["old_values"])
	if err != nil {
		return p, err
	}
	if err = validateWarehouseDeviceDraft(ctx, db, &p, &previous, in.DeviceID, false); err != nil {
		return p, err
	}
	if err = previewDeviceEditDependencies(ctx, db, &p, in.DeviceID, current, previous); err != nil {
		return p, err
	}
	deviceDiff(&p, current, previous)
	p.Draft = deviceDraftMap(previous)
	p.Draft["audit_id"] = in.AuditID
	p.Draft["expected_updated_at"] = p.Current["updated_at"]
	p.Draft["confirmation_text_required"] = fmt.Sprintf("REVERT WAREHOUSE DEVICE %s UPDATE %d", in.DeviceID, in.AuditID)
	p.Warnings = append(p.Warnings, "Only this inverse field edit is applied. Audit remains; later device writes or audits block revert. Location, condition and job processes are not undone.")
	p.finish()
	return p, nil
}

func validateWarehouseDeviceDraft(ctx context.Context, db *store.Store, p *preparedMutation, d *warehouseDeviceDraft, id string, create bool) error {
	for _, f := range []struct {
		name  string
		value **string
		max   int
	}{{"serial_number", &d.SerialNumber, 255}, {"barcode", &d.Barcode, 255}, {"qr_code", &d.QRCode, 255}, {"notes", &d.Notes, 4000}} {
		if *f.value != nil {
			v := strings.TrimSpace(**f.value)
			if len([]rune(v)) > f.max {
				p.require(f.name, "Text überschreitet die maximale Länge.", nil)
			}
			if v == "" {
				*f.value = nil
			} else {
				*f.value = &v
			}
		}
	}
	if !create && (d.Barcode == nil || d.QRCode == nil) {
		p.require("scan_codes", "Barcode und QR-Code dürfen nicht leer sein.", nil)
	}
	if math.IsNaN(d.ConditionRating) || math.IsInf(d.ConditionRating, 0) || d.ConditionRating < 0 || d.ConditionRating > 5 || math.Abs(d.ConditionRating*10-math.Round(d.ConditionRating*10)) > 0.00001 {
		p.require("condition_rating", "Wert 0–5 mit höchstens einer Nachkommastelle erforderlich.", nil)
	}
	if math.IsNaN(d.UsageHours) || math.IsInf(d.UsageHours, 0) || d.UsageHours < 0 || d.UsageHours > 99999999.99 || math.Abs(d.UsageHours*100-math.Round(d.UsageHours*100)) > 0.00001 {
		p.require("usage_hours", "Nichtnegative Stunden mit höchstens zwei Nachkommastellen erforderlich.", nil)
	}
	for _, f := range []struct {
		name  string
		value **string
	}{{"purchase_date", &d.PurchaseDate}, {"last_maintenance", &d.LastMaintenance}, {"next_maintenance", &d.NextMaintenance}} {
		if *f.value != nil {
			v := strings.TrimSpace(**f.value)
			if v == "" {
				*f.value = nil
			} else {
				*f.value = &v
				if _, err := time.Parse("2006-01-02", v); err != nil {
					p.require(f.name, "Gültiges Datum im Format YYYY-MM-DD erforderlich.", nil)
				}
			}
		}
	}
	if d.LastMaintenance != nil && d.NextMaintenance != nil && *d.LastMaintenance > *d.NextMaintenance {
		p.require("next_maintenance", "Nächste Wartung darf nicht vor letzter Wartung liegen.", nil)
	}
	if d.ProductID <= 0 || d.ProductID > math.MaxInt32 {
		p.require("product_id", "Aktives, einzeln verfolgtes Produkt erforderlich.", nil)
	} else {
		rows, err := db.Query(ctx, `SELECT productid AS product_id,name,tracking_mode,lifecycle_status FROM products WHERE productid=$1`, d.ProductID)
		if err != nil {
			return err
		}
		p.RelatedRecords = append(p.RelatedRecords, rows...)
		if len(rows) != 1 || rows[0]["tracking_mode"] != "individual" || rows[0]["lifecycle_status"] != "active" {
			p.require("product_id", "Aktives, einzeln verfolgtes Produkt erforderlich.", rows)
		}
	}
	if d.SerialNumber != nil {
		rows, err := db.Query(ctx, `SELECT deviceid AS device_id,lifecycle_status FROM devices WHERE deviceid<>$1 AND lower(trim(serialnumber))=lower($2) LIMIT 5`, id, *d.SerialNumber)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			p.require("duplicate_serial", "Seriennummer gehört bereits einem anderen Gerät, auch archivierte Geräte reservieren sie.", rows)
		}
	}
	codes := []*string{d.Barcode, d.QRCode}
	if id != "" {
		codes = append(codes, &id)
	}
	for _, code := range codes {
		if code == nil {
			continue
		}
		rows, err := db.Query(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE deviceid<>$1 AND (lower(trim(deviceid))=lower($2) OR lower(trim(barcode))=lower($2) OR lower(trim(qr_code))=lower($2))) OR EXISTS(SELECT 1 FROM inventory_identifiers WHERE NOT(entity_type='device' AND entity_key=$1) AND lower(trim(code))=lower($2)) AS conflict`, id, *code)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0]["conflict"] == true {
			p.require("identifier_conflict", "Scan-Kennung ist bereits für einen anderen Bestand reserviert.", *code)
		}
	}
	p.Warnings = append(p.Warnings, "Serial numbers and scan codes remain reserved while archived. Changed physical labels must be reviewed separately; maintenance dates do not complete orders or alter plans.")
	return nil
}

func previewDeviceDestination(ctx context.Context, db *store.Store, p *preparedMutation, id int64) error {
	rows, err := db.Query(ctx, `SELECT zone_id,code,name,is_active,is_storable,operational_status,capacity,
 (SELECT count(*) FROM devices WHERE zone_id=$1 AND status='in_storage' AND lifecycle_status='active')+(SELECT count(*) FROM cases WHERE zone_id=$1)+COALESCE((SELECT sum(quantity) FROM product_locations WHERE zone_id=$1),0) AS used
 FROM storage_zones WHERE zone_id=$1`, id)
	if err != nil {
		return err
	}
	p.RelatedRecords = append(p.RelatedRecords, rows...)
	if len(rows) != 1 {
		p.require("zone_id", "Lagerplatz wurde nicht gefunden.", nil)
		return nil
	}
	zone := rows[0]
	if zone["is_active"] != true || zone["is_storable"] != true || zone["operational_status"] != "available" {
		p.require("zone_id", "Lagerplatz muss aktiv, belegbar und verfügbar sein.", zone)
	}
	if zone["capacity"] != nil && numericValue(zone["used"])+1 > numericValue(zone["capacity"]) {
		p.require("zone_id", "Lagerplatz hat keine freie Kapazität.", zone)
	}
	ancestors, err := db.Query(ctx, `WITH RECURSIVE ancestors AS (
 SELECT zone_id,parent_zone_id,is_active,operational_status,ARRAY[zone_id] AS path,false AS cycle FROM storage_zones WHERE zone_id=$1
 UNION ALL SELECT z.zone_id,z.parent_zone_id,z.is_active,z.operational_status,a.path||z.zone_id,z.zone_id=ANY(a.path) FROM storage_zones z JOIN ancestors a ON z.zone_id=a.parent_zone_id WHERE NOT a.cycle AND cardinality(a.path)<100
 ) SELECT EXISTS(SELECT 1 FROM ancestors WHERE NOT is_active OR operational_status='archived' OR cycle OR cardinality(path)>=100) AS invalid`, id)
	if err != nil {
		return err
	}
	if len(ancestors) != 1 || ancestors[0]["invalid"] == true {
		p.require("zone_id", "Lagerhierarchie ist archiviert oder ungültig.", nil)
	}
	return nil
}
func numericValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		fmt.Sscan(n, &f)
		return f
	}
	return 0
}

func registerWarehouseDeviceAuditTool(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.devices.audit_history", "Review device audit history", "Read redacted device lifecycle and field-edit events, versions and eligibility of your latest unchanged MCP update. Notes, raw JSON, IP and user-agent are excluded. Warehouse administrator required.", func(ctx context.Context, in WarehouseDeviceAuditInput) (any, []Source, []string, error) {
		return warehouseDeviceAuditHistory(ctx, db, in)
	})
}
func warehouseDeviceAuditHistory(ctx context.Context, db *store.Store, in WarehouseDeviceAuditInput) (any, []Source, []string, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if !exactDeviceID(in.DeviceID) {
		return nil, nil, nil, fmt.Errorf("exact device_id is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	actor := auth.TokenInfoFromContext(ctx)
	rows, err := db.Query(ctx, `SELECT a.id AS audit_id,a.action,a.user_id,a.timestamp AS changed_at,COALESCE(a.new_values->>'origin','UI') AS origin,
 a.new_values->>'updated_at' AS result_version,a.new_values->>'reverted_audit_id' AS reverted_audit_id,
 COALESCE(a.old_values->>'product_id',a.old_values#>>'{fields,product_id}') AS product_id_before,a.new_values#>>'{after,product_id}' AS product_id_after,
 COALESCE(a.old_values->>'barcode',a.old_values#>>'{fields,barcode}') AS barcode_before,a.new_values#>>'{after,barcode}' AS barcode_after,
 COALESCE(a.old_values->>'qr_code',a.old_values#>>'{fields,qr_code}') AS qr_code_before,a.new_values#>>'{after,qr_code}' AS qr_code_after,
 a.old_values->>'lifecycle_status' AS lifecycle_status_before,a.new_values->>'lifecycle_status' AS lifecycle_status_after,
 COALESCE(a.old_values->'notes',a.old_values#>'{fields,notes}') IS DISTINCT FROM a.new_values#>'{after,notes}' AS notes_changed,
 COALESCE(a.action='device.update' AND a.new_values->>'origin'='MCP/AI' AND a.user_id::text=$2 AND a.id=(SELECT max(id) FROM audit_log WHERE entity_type='device' AND entity_id=$1) AND EXISTS(SELECT 1 FROM devices d WHERE d.deviceid=$1 AND d.lifecycle_status='active' AND to_char(d.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')=a.new_values->>'updated_at'),false) AS can_prepare_revert
 FROM audit_log a WHERE a.entity_type='device' AND a.entity_id=$1 ORDER BY a.id DESC LIMIT $3`, in.DeviceID, actor.UserID, limit)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{"device_id": in.DeviceID, "events": rows}, deviceSources(in.DeviceID), nil, nil
}

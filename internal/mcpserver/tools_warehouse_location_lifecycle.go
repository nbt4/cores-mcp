package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseLocationLifecycleInput struct {
	MutationControl
	ZoneID            int64  `json:"zone_id,omitempty" jsonschema:"Exact existing location ID, active for archive or inactive for restore."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact location version from lifecycle preview."`
	ConfirmLifecycle  bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after reviewing lifecycle, operational status and all dependencies with explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact location-bound phrase from preview."`
}
type WarehouseLocationAuditInput struct {
	ZoneID int64 `json:"zone_id" jsonschema:"Exact location ID, including archived locations."`
	Limit  int   `json:"limit,omitempty" jsonschema:"Maximum redacted events, capped at 100."`
}

const warehouseLocationLifecycleDependenciesSQL = `WITH RECURSIVE descendants AS (
 SELECT zone_id,is_active FROM storage_zones WHERE parent_zone_id=$1
 UNION SELECT z.zone_id,z.is_active FROM storage_zones z JOIN descendants d ON z.parent_zone_id=d.zone_id
) SELECT
 (SELECT count(*) FROM descendants WHERE is_active) AS active_descendants,
 (SELECT count(*) FROM devices WHERE zone_id=$1 AND COALESCE(lifecycle_status,'active')='active') AS active_devices,
 (SELECT count(*) FROM cases WHERE zone_id=$1) AS cases,
 (SELECT count(*) FROM cases WHERE home_zone_id=$1) AS home_cases,
 (SELECT count(*) FROM product_locations WHERE zone_id=$1 AND quantity<>0) AS stock_lines,
 (SELECT count(*) FROM warehouse_tasks WHERE (from_zone_id=$1 OR to_zone_id=$1) AND lower(COALESCE(status,'')) NOT IN ('done','cancelled')) AS open_tasks,
 (SELECT count(*) FROM inventory_counts WHERE zone_id=$1 AND lower(COALESCE(status,'')) NOT IN ('approved','cancelled')) AS open_counts,
 (SELECT count(*) FROM inventory_counts WHERE zone_id=$1) AS historic_counts`

const warehouseLocationRestoreStatusSQL = `SELECT COALESCE((SELECT CASE WHEN old_values->>'operational_status' IN ('available','blocked','maintenance') THEN old_values->>'operational_status' ELSE 'blocked' END
 FROM audit_log WHERE entity_type='storage_zone' AND entity_id=$1 AND action='storage_zone.archive' AND new_values->>'updated_at'=$2 ORDER BY id DESC LIMIT 1),'blocked') AS restore_status`

const warehouseLocationLifecycleSnapshotSQL = `SELECT zone_id,code,barcode,name,type::text,location_kind,process_role,description,parent_zone_id,capacity,capacity_mode,is_storable,pick_sequence,max_weight_kg,max_volume_m3,inventory_frequency_days,last_counted_at,next_count_at,profile_id,operational_status,is_active,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM storage_zones WHERE zone_id=$1`

func locationLifecycleSources(id int64) []Source {
	return []Source{{Service: "warehousecore", Entity: "storage_zone", ID: fmt.Sprint(id)}}
}
func registerWarehouseLocationLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "warehouse.locations.prepare_"+op, "Prepare location "+op, "Preview the full location and exact version, operational-state diff, inventory, descendants, home cases, open tasks/counts and historical counts. Restore revalidates hierarchy and scan identity. Admin/archive scope required.", func(ctx context.Context, in WarehouseLocationLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseLocationLifecycle(ctx, db, in, op)
			return p.response("draft"), locationLifecycleSources(in.ZoneID), p.Warnings, err
		})
		addUpdateTool(server, "warehouse.locations."+op, "Location "+op, "Archive or restore with exact version, admin/archive scope and location-bound confirmation. Preserve hierarchy, inventory history and metadata; never move stock or reactivate children. Restore an unchanged audited operational state, otherwise blocked. WarehouseCore rechecks dependencies and commits audit/replay atomically.", func(ctx context.Context, in WarehouseLocationLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseLocationLifecycle(ctx, db, in, op)
			if err != nil || !p.Ready {
				return p.response("needs_input"), locationLifecycleSources(in.ZoneID), p.Warnings, err
			}
			if !in.ConfirmLifecycle {
				return p.response("confirmation_required"), locationLifecycleSources(in.ZoneID), p.Warnings, nil
			}
			phrase := fmt.Sprintf("%s WAREHOUSE LOCATION %d", strings.ToUpper(op), in.ZoneID)
			if in.ConfirmationText != phrase {
				return p.response("elevated_confirmation_required"), locationLifecycleSources(in.ZoneID), p.Warnings, nil
			}
			var result map[string]any
			err = api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/warehouse/locations/%d/%s", in.ZoneID, op), http.MethodPost, map[string]any{"expected_updated_at": in.ExpectedUpdatedAt, "confirm_lifecycle": true, "confirmation_text": phrase}, &result)
			if err != nil {
				return nil, nil, nil, err
			}
			return map[string]any{"operation_status": op + "d", "location": result, "diff": p.Diff}, locationLifecycleSources(in.ZoneID), p.Warnings, nil
		})
	}
}
func prepareWarehouseLocationLifecycle(ctx context.Context, db *store.Store, in WarehouseLocationLifecycleInput, op string) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	if op != "archive" && op != "restore" {
		return p, fmt.Errorf("invalid location lifecycle action")
	}
	if in.ZoneID <= 0 || in.ZoneID > math.MaxInt32 {
		p.require("zone_id", "Exakte gültige Lagerplatz-ID erforderlich.", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, warehouseLocationLifecycleSnapshotSQL, in.ZoneID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("zone_id", "Lagerplatz wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	from, to := true, false
	if op == "restore" {
		from, to = to, from
	}
	if p.Current["is_active"] != from {
		p.require("is_active", "Archivstatus erlaubt diesen Wechsel nicht.", nil)
	}
	version := nullableText(p.Current["updated_at"])
	if in.ConfirmLifecycle && in.ExpectedUpdatedAt == "" || in.ExpectedUpdatedAt != "" && in.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Neue Vorschau mit unveränderter Lagerplatzversion erforderlich.", version)
	}
	dependencies, err := db.Query(ctx, warehouseLocationLifecycleDependenciesSQL, in.ZoneID)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = dependencies
	if len(dependencies) != 1 {
		return p, fmt.Errorf("location dependencies unavailable")
	}
	for _, key := range []string{"active_descendants", "active_devices", "cases", "home_cases", "stock_lines", "open_tasks", "open_counts"} {
		if numericID(dependencies[0][key]) > 0 {
			p.require(key, "Aktive Abhängigkeiten zuerst auflösen.", dependencies[0][key])
		}
	}
	status := "archived"
	if op == "archive" && (p.Current["operational_status"] == "counting" || p.Current["operational_status"] == "archived") {
		p.require("operational_status", "Inventur abschließen; inkonsistenten Archivstatus zuerst prüfen.", nil)
	}
	if op == "restore" {
		p.Draft = cloneMap(p.Current)
		validateWarehouseLocationUpdateDraft(&p)
		parent := p.Current["parent_zone_id"]
		if parent != nil {
			ancestors, err := db.Query(ctx, `WITH RECURSIVE ancestors AS (SELECT zone_id,parent_zone_id,is_active,operational_status,ARRAY[zone_id] AS path,false AS cycle FROM storage_zones WHERE zone_id=$1 UNION ALL SELECT z.zone_id,z.parent_zone_id,z.is_active,z.operational_status,a.path||z.zone_id,z.zone_id=ANY(a.path) FROM storage_zones z JOIN ancestors a ON z.zone_id=a.parent_zone_id WHERE NOT a.cycle) SELECT a.zone_id AS id,a.parent_zone_id,a.is_active,a.operational_status,a.cycle,(a.parent_zone_id IS NOT NULL AND p.zone_id IS NULL) AS parent_missing FROM ancestors a LEFT JOIN storage_zones p ON p.zone_id=a.parent_zone_id`, parent)
			if err != nil {
				return p, err
			}
			if len(ancestors) == 0 {
				p.require("parent_zone_id", "Elternknoten fehlt.", parent)
			}
			for _, row := range ancestors {
				p.RelatedRecords = append(p.RelatedRecords, row)
				if numericID(row["id"]) == in.ZoneID || row["is_active"] != true || row["operational_status"] == "archived" || row["cycle"] == true || row["parent_missing"] == true {
					p.require("parent_zone_id", "Elternhierarchie ist inaktiv oder zyklisch.", row)
				}
			}
		}
		duplicates, err := db.Query(ctx, `SELECT zone_id AS id,code,barcode,name FROM storage_zones WHERE zone_id<>$1 AND (lower(trim(code))=lower(trim($2)) OR lower(trim(barcode))=lower(trim($3)) OR (lower(trim(name))=lower(trim($4)) AND parent_zone_id IS NOT DISTINCT FROM $5)) LIMIT 10`, in.ZoneID, p.Current["code"], p.Current["barcode"], p.Current["name"], parent)
		if err != nil {
			return p, err
		}
		if len(duplicates) > 0 {
			p.require("duplicate_location", "Lagerplatz-Identität ist bereits vergeben.", duplicates)
			p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		}
		states, err := db.Query(ctx, warehouseLocationRestoreStatusSQL, fmt.Sprint(in.ZoneID), version)
		if err != nil {
			return p, err
		}
		if len(states) != 1 {
			return p, fmt.Errorf("restore state unavailable")
		}
		status = nullableText(states[0]["restore_status"])
	}
	p.Draft = map[string]any{"zone_id": in.ZoneID, "expected_updated_at": version, "confirmation_text_required": fmt.Sprintf("%s WAREHOUSE LOCATION %d", strings.ToUpper(op), in.ZoneID), "operational_status": status}
	p.Diff = map[string]map[string]any{"is_active": {"before": from, "after": to}, "operational_status": {"before": p.Current["operational_status"], "after": status}}
	p.Warnings = append(p.Warnings, "Restore preserves available/blocked/maintenance only for an unchanged audited archive. Legacy archives or later edits restore blocked and require operational review in WarehouseCore. No stock is moved, no children are restored, and scan identity, metadata and histories remain.")
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	p.finish()
	return p, nil
}
func registerWarehouseLocationAuditTool(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.locations.audit_history", "Review location changes", "Warehouse administrator read-only access to redacted location create/update/archive/restore events and versions. Descriptions, raw JSON, IP and user-agent are excluded.", func(ctx context.Context, in WarehouseLocationAuditInput) (any, []Source, []string, error) {
		return warehouseLocationAuditHistory(ctx, db, in)
	})
}
func warehouseLocationAuditHistory(ctx context.Context, db *store.Store, in WarehouseLocationAuditInput) (any, []Source, []string, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if in.ZoneID <= 0 || in.ZoneID > math.MaxInt32 {
		return nil, nil, nil, fmt.Errorf("valid zone_id is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.Query(ctx, `SELECT id AS audit_id,action,user_id,timestamp AS changed_at,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,
 old_values->>'name' AS name_before,COALESCE(new_values#>>'{after,name}',new_values->>'name') AS name_after,
 old_values->>'is_active' AS is_active_before,COALESCE(new_values#>>'{after,is_active}',new_values->>'is_active') AS is_active_after,
 old_values->>'operational_status' AS operational_status_before,COALESCE(new_values#>>'{after,operational_status}',new_values->>'operational_status') AS operational_status_after
 FROM audit_log WHERE entity_type='storage_zone' AND entity_id=$1 ORDER BY id DESC LIMIT $2`, fmt.Sprint(in.ZoneID), limit)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{"zone_id": in.ZoneID, "events": rows}, locationLifecycleSources(in.ZoneID), nil, nil
}

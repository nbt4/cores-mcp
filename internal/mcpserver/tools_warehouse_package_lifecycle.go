package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehousePackageLifecycleInput struct {
	MutationControl
	PackageID         int64  `json:"package_id,omitempty" jsonschema:"Exact existing package ID, active for archive or inactive for restore."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact package metadata/content version from lifecycle preview."`
	ConfirmLifecycle  bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after reviewing lifecycle, website visibility and all dependencies with explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact package-bound phrase from preview."`
}
type WarehousePackageAuditInput struct {
	PackageID int64 `json:"package_id" jsonschema:"Exact package ID, including archived packages."`
	Limit     int   `json:"limit,omitempty" jsonschema:"Maximum redacted events, capped at 100."`
}

const warehousePackageLifecycleDependenciesSQL = `SELECT
 (SELECT count(*) FROM job_packages jp LEFT JOIN jobs j ON j.jobid=jp.job_id LEFT JOIN status s ON s.statusid=j.statusid WHERE jp.package_id=$1 AND j.deleted_at IS NULL AND NOT warehouse_job_status_is_closed(COALESCE(s.status,''))) AS active_jobs,
 (SELECT count(*) FROM job_package_reservations r JOIN job_packages jp ON jp.job_package_id=r.job_package_id WHERE jp.package_id=$1 AND r.reservation_status<>'released') AS open_reservations,
 (SELECT count(*) FROM job_packages WHERE package_id=$1) AS historic_job_uses`
const warehousePackageLifecycleSnapshotSQL = `SELECT id,COALESCE(NULLIF(package_code,''),code,'') AS package_code,COALESCE(is_active,true) AS is_active,
 to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,
 jsonb_build_object('name',name,'description',description,'price',price,'category',category,'website_visible',COALESCE(website_visible,false),'aliases',COALESCE(NULLIF(alias_json,''),'[]')::jsonb,
 'items',COALESCE((SELECT jsonb_agg(jsonb_build_object('product_id',product_id,'quantity',COALESCE(quantity,1),'is_optional',COALESCE(is_optional,false)) ORDER BY product_id,id) FROM product_package_items WHERE package_id=$1),'[]'::jsonb)) AS fields
 FROM product_packages WHERE id=$1`

func packageLifecycleSources(id int64) []Source {
	return []Source{{Service: "warehousecore", Entity: "package", ID: fmt.Sprint(id)}}
}
func registerWarehousePackageLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "warehouse.packages.prepare_"+op, "Prepare package "+op, "Preview exact metadata/content version, full package, lifecycle/website diff and active jobs/reservations. Restore revalidates every product. Admin and archive scope required.", func(ctx context.Context, in WarehousePackageLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehousePackageLifecycle(ctx, db, in, op)
			return p.response("draft"), packageLifecycleSources(in.PackageID), p.Warnings, err
		})
		addUpdateTool(server, "warehouse.packages."+op, "Package "+op, "Archive or restore with exact version, admin/archive scope and package-bound confirmation. Keep history, prices and item row IDs; disable public website visibility. WarehouseCore rechecks dependencies and atomically records audit and durable replay.", func(ctx context.Context, in WarehousePackageLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehousePackageLifecycle(ctx, db, in, op)
			if err != nil || !p.Ready {
				return p.response("needs_input"), packageLifecycleSources(in.PackageID), p.Warnings, err
			}
			if !in.ConfirmLifecycle {
				return p.response("confirmation_required"), packageLifecycleSources(in.PackageID), p.Warnings, nil
			}
			phrase := fmt.Sprintf("%s WAREHOUSE PACKAGE %d", strings.ToUpper(op), in.PackageID)
			if in.ConfirmationText != phrase {
				return p.response("elevated_confirmation_required"), packageLifecycleSources(in.PackageID), p.Warnings, nil
			}
			var result map[string]any
			err = api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/product-packages/%d/%s", in.PackageID, op), http.MethodPost, map[string]any{"expected_updated_at": in.ExpectedUpdatedAt, "confirm_lifecycle": true, "confirmation_text": phrase}, &result)
			if err != nil {
				return nil, nil, nil, err
			}
			return map[string]any{"operation_status": op + "d", "package": result, "diff": p.Diff}, packageLifecycleSources(in.PackageID), p.Warnings, nil
		})
	}
}
func prepareWarehousePackageLifecycle(ctx context.Context, db *store.Store, in WarehousePackageLifecycleInput, op string) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	if op != "archive" && op != "restore" {
		return p, fmt.Errorf("invalid package lifecycle action")
	}
	if in.PackageID <= 0 || in.PackageID > math.MaxInt32 {
		p.require("package_id", "Exakte gültige Paket-ID erforderlich.", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, warehousePackageLifecycleSnapshotSQL, in.PackageID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("package_id", "Paket wurde nicht gefunden.", nil)
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
		p.require("expected_updated_at", "Neue Vorschau mit unveränderter Paketversion erforderlich.", version)
	}
	var fields warehousePackageDraft
	raw, err := json.Marshal(p.Current["fields"])
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(raw, &fields); err != nil {
		return p, err
	}
	dependencies, err := db.Query(ctx, warehousePackageLifecycleDependenciesSQL, in.PackageID)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = dependencies
	if len(dependencies) != 1 || numericID(dependencies[0]["active_jobs"]) > 0 || numericID(dependencies[0]["open_reservations"]) > 0 {
		p.require("active_dependencies", "Aktive Jobs oder offene Reservierungen zuerst auflösen.", dependencies)
	}
	if op == "restore" {
		if err = validateWarehousePackageDraft(ctx, db, &p, &fields, in.PackageID, true); err != nil {
			return p, err
		}
	}
	p.Draft = map[string]any{"package_id": in.PackageID, "expected_updated_at": version, "confirmation_text_required": fmt.Sprintf("%s WAREHOUSE PACKAGE %d", strings.ToUpper(op), in.PackageID)}
	p.Diff = map[string]map[string]any{"is_active": {"before": from, "after": to}}
	if fields.WebsiteVisible {
		p.Diff["website_visible"] = map[string]any{"before": true, "after": false}
	}
	p.Warnings = append(p.Warnings, "Both lifecycle actions disable website visibility. Restoration does not republish; publish separately through a confirmed update. Package fields, item row IDs and historic job composition remain. No products or stock are moved or deleted.")
	p.finish()
	return p, nil
}
func registerWarehousePackageAuditTool(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.packages.audit_history", "Review package changes", "Warehouse administrator read-only access to redacted package create/update/archive/restore history, versions and visibility. Raw JSON, descriptions, IP and user-agent are excluded.", func(ctx context.Context, in WarehousePackageAuditInput) (any, []Source, []string, error) {
		return warehousePackageAuditHistory(ctx, db, in)
	})
}
func warehousePackageAuditHistory(ctx context.Context, db *store.Store, in WarehousePackageAuditInput) (any, []Source, []string, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if in.PackageID <= 0 || in.PackageID > math.MaxInt32 {
		return nil, nil, nil, fmt.Errorf("valid package_id is required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.Query(ctx, `SELECT id AS audit_id,action,user_id,timestamp AS changed_at,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,
 old_values->>'name' AS name_before,new_values#>>'{after,name}' AS name_after,old_values->>'is_active' AS is_active_before,new_values->>'is_active' AS is_active_after,
 old_values->>'website_visible' AS website_visible_before,new_values#>>'{after,website_visible}' AS website_visible_after
 FROM audit_log WHERE entity_type='package' AND entity_id=$1 ORDER BY id DESC LIMIT $2`, fmt.Sprint(in.PackageID), limit)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{"package_id": in.PackageID, "events": rows}, packageLifecycleSources(in.PackageID), nil, nil
}

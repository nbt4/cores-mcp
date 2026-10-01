package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseProductLifecycleInput struct {
	MutationControl
	ProductID         int64  `json:"product_id,omitempty" jsonschema:"Exact WarehouseCore product ID."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact version from the lifecycle preview."`
	ConfirmLifecycle  bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after reviewing the affected devices and dependencies."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Type the exact record-bound phrase from the preview."`
}

func registerWarehouseProductLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"archive", "restore"} {
		operation := operation
		prepare := func(ctx context.Context, input WarehouseProductLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseProductLifecycle(ctx, db, input, operation)
			return p.response("draft"), warehouseLifecycleSources(input.ProductID), p.Warnings, err
		}
		addWritePreparationTool(server, "warehouse.products.prepare_"+operation, "Prepare warehouse product "+operation, "Preview the exact product version, lifecycle diff, affected devices and active job dependencies without changing data.", prepare)
		addUpdateTool(server, "warehouse.products."+operation, "Warehouse product "+operation, "Archive or restore one product after admin scope, exact version, dependency review and record-bound confirmation. WarehouseCore commits audit and idempotency receipt with the change.", func(ctx context.Context, input WarehouseProductLifecycleInput) (any, []Source, []string, error) {
			p, err := prepareWarehouseProductLifecycle(ctx, db, input, operation)
			if err != nil || !p.Ready {
				return p.response("needs_input"), warehouseLifecycleSources(input.ProductID), p.Warnings, err
			}
			if !input.ConfirmLifecycle {
				return p.response("confirmation_required"), warehouseLifecycleSources(input.ProductID), append(p.Warnings, "No data was changed."), nil
			}
			if strings.TrimSpace(input.ConfirmationText) != warehouseLifecyclePhrase(operation, input.ProductID) {
				return p.response("elevated_confirmation_required"), warehouseLifecycleSources(input.ProductID), append(p.Warnings, "No data was changed. Type the phrase from the preview."), nil
			}
			method, path := http.MethodDelete, fmt.Sprintf("/api/v1/admin/products/%d", input.ProductID)
			if operation == "restore" {
				method, path = http.MethodPut, path+"/restore"
			}
			var result map[string]any
			if err := api.doJSON(ctx, cfg.WarehouseURL, path, method, map[string]any{"expectedUpdatedAt": input.ExpectedUpdatedAt}, &result); err != nil {
				return nil, nil, nil, err
			}
			return map[string]any{"operation_status": operation + "d", "product": result, "diff": p.Diff}, warehouseLifecycleSources(input.ProductID), p.Warnings, nil
		})
	}
}

func warehouseLifecycleSources(id int64) []Source {
	return []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(id)}}
}

func warehouseLifecyclePhrase(operation string, id int64) string {
	if id <= 0 || operation != "archive" && operation != "restore" {
		return ""
	}
	return fmt.Sprintf("%s WAREHOUSE PRODUCT %d", strings.ToUpper(operation), id)
}

func prepareWarehouseProductLifecycle(ctx context.Context, db *store.Store, input WarehouseProductLifecycleInput, operation string) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Warehouse administrator permission is required for product lifecycle changes")
	}
	if input.ProductID <= 0 {
		p.require("product_id", "Welche gültige Produkt-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	if operation != "archive" && operation != "restore" {
		return p, fmt.Errorf("unsupported lifecycle operation")
	}
	rows, err := db.Query(ctx, `SELECT productid AS product_id,name,product_code,lifecycle_status,website_visible,website_featured,
		to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM products WHERE productid=$1`, input.ProductID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("product_id", "Produkt nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	from, to := "active", "archived"
	if operation == "restore" {
		from, to = to, from
	}
	if p.Current["lifecycle_status"] != from {
		p.require("lifecycle_status", "Der aktuelle Produktstatus erlaubt diesen Wechsel nicht.", p.Current["lifecycle_status"])
	}
	version := fmt.Sprint(p.Current["updated_at"])
	if input.ConfirmLifecycle && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Die exakte Version aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Produkt wurde seit der Vorschau geändert.", version)
	}
	deviceCondition := "lifecycle_status='active'"
	if operation == "restore" {
		deviceCondition = "lifecycle_status='archived' AND archived_by_product=TRUE"
	}
	devices, err := db.Query(ctx, `SELECT COUNT(*) AS affected_devices FROM devices WHERE productid=$1 AND `+deviceCondition, input.ProductID)
	if err != nil {
		return p, err
	}
	count := numericID(devices[0]["affected_devices"])
	p.Diff = map[string]map[string]any{"lifecycle_status": {"before": from, "after": to}, "affected_devices": {"before": count, "after": map[string]any{"lifecycle_status": to, "count": count}}}
	if operation == "archive" {
		p.Diff["website_visible"] = map[string]any{"before": p.Current["website_visible"], "after": false}
		p.Diff["website_featured"] = map[string]any{"before": p.Current["website_featured"], "after": false}
		deps, err := db.Query(ctx, `SELECT
			(SELECT COUNT(*) FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id JOIN status s ON s.statusid=j.statusid
			 WHERE r.product_id=$1 AND j.deleted_at IS NULL AND lower(trim(s.status)) NOT IN ('abgeschlossen','storniert','completed','paid','canceled','cancelled','abgerechnet')) AS open_job_requirements,
			(SELECT COUNT(*) FROM job_devices jd JOIN devices d ON d.deviceid=jd.deviceid WHERE d.productid=$1 AND jd.pack_status IN ('packed','issued')) AS packed_or_issued_devices`, input.ProductID)
		if err != nil {
			return p, err
		}
		p.RelatedRecords = deps
		if numericID(deps[0]["open_job_requirements"]) > 0 || numericID(deps[0]["packed_or_issued_devices"]) > 0 {
			p.require("active_dependencies", "Produkt wird noch von offenen Jobs oder gepackten Geräten verwendet.", deps[0])
		}
		p.Warnings = append(p.Warnings, "Archiving also archives active devices and disables their inventory identifiers; website visibility is cleared.")
	} else {
		parents, parentErr := db.Query(ctx, `SELECT p.manufacturerid AS manufacturer_id,p.brandid AS brand_id,
		 (p.manufacturerid IS NULL OR COALESCE(m.lifecycle_status='active',false)) AS manufacturer_active,
		 (p.brandid IS NULL OR COALESCE(b.lifecycle_status='active',false)) AS brand_active
		 FROM products p LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid LEFT JOIN brands b ON b.brandid=p.brandid WHERE p.productid=$1`, input.ProductID)
		if parentErr != nil {
			return p, parentErr
		}
		if len(parents) != 1 || parents[0]["manufacturer_active"] != true || parents[0]["brand_active"] != true {
			p.require("active_master_data", "Hersteller und Marke zuerst wiederherstellen; historische Zuordnungen bleiben erhalten.", parents)
		}
		p.Warnings = append(p.Warnings, "Restoring reactivates only devices archived by this product; website visibility remains unchanged.")
	}
	p.Draft = map[string]any{"product_id": input.ProductID, "expected_updated_at": version, "confirmation_text_required": warehouseLifecyclePhrase(operation, input.ProductID), "affected_devices": count}
	p.finish()
	return p, nil
}

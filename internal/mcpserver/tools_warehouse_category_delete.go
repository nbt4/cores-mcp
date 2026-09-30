package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCategoryDeleteInput struct {
	MutationControl
	CategoryID        int64  `json:"category_id,omitempty" jsonschema:"Exact unused top-level category ID to permanently remove."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact unchanged version from prepare_delete."`
	ConfirmDelete     bool   `json:"confirm_delete,omitempty" jsonschema:"Set only after showing the complete record, dependencies and permanent removal preview, and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound deletion phrase from prepare_delete."`
}

type WarehouseSubcategoryDeleteInput struct {
	MutationControl
	SubcategoryID     string `json:"subcategory_id,omitempty" jsonschema:"Exact unused second-level category ID to permanently remove."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact unchanged version from prepare_delete."`
	ConfirmDelete     bool   `json:"confirm_delete,omitempty" jsonschema:"Set only after showing the complete record, dependencies and permanent removal preview, and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound deletion phrase from prepare_delete."`
}

type WarehouseThirdCategoryDeleteInput struct {
	MutationControl
	ThirdCategoryID   string `json:"third_category_id,omitempty" jsonschema:"Exact unused third-level ID, returned as subbiercategory_id by create, to permanently remove."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact unchanged version from prepare_delete."`
	ConfirmDelete     bool   `json:"confirm_delete,omitempty" jsonschema:"Set only after showing the complete record, dependencies and permanent removal preview, and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound deletion phrase from prepare_delete."`
}

func registerWarehouseCategoryDeleteTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	registerWarehouseCategoryDeletePair(server, cfg, db, "categories", "category", func(in WarehouseCategoryDeleteInput) (any, string, bool, string) {
		return in.CategoryID, in.ExpectedUpdatedAt, in.ConfirmDelete, in.ConfirmationText
	})
	registerWarehouseCategoryDeletePair(server, cfg, db, "subcategories", "subcategory", func(in WarehouseSubcategoryDeleteInput) (any, string, bool, string) {
		return strings.TrimSpace(in.SubcategoryID), in.ExpectedUpdatedAt, in.ConfirmDelete, in.ConfirmationText
	})
	registerWarehouseCategoryDeletePair(server, cfg, db, "third_categories", "third_category", func(in WarehouseThirdCategoryDeleteInput) (any, string, bool, string) {
		return strings.TrimSpace(in.ThirdCategoryID), in.ExpectedUpdatedAt, in.ConfirmDelete, in.ConfirmationText
	})
}

func registerWarehouseCategoryDeletePair[In any](server *mcp.Server, cfg config.Config, db *store.Store, plural, kind string, fields func(In) (any, string, bool, string)) {
	api := newCoreAPIClient(cfg)
	source := func(id any) []Source { return []Source{{Service: "warehousecore", Entity: kind, ID: fmt.Sprint(id)}} }
	addWritePreparationTool(server, "warehouse."+plural+".prepare_delete", "Prepare warehouse "+kind+" deletion", "Preview the complete category, exact version and dependencies. Only unused categories without children may be permanently removed; show the exact record-bound deletion phrase. Requires Warehouse administrator and dedicated delete scope.", func(ctx context.Context, in In) (any, []Source, []string, error) {
		id, expected, confirmed, _ := fields(in)
		p, err := prepareWarehouseCategoryDelete(ctx, db, kind, id, expected, confirmed)
		return p.response("draft"), source(id), p.Warnings, err
	})
	addUpdateTool(server, "warehouse."+plural+".delete", "Delete unused warehouse "+kind, "Permanently remove only one unused category without children after exact version, dependency preview, explicit confirmation and exact record-bound phrase. WarehouseCore rechecks dependencies and commits deletion, audit and durable replay receipt together. No cascade or automatic reassignment.", func(ctx context.Context, in In) (any, []Source, []string, error) {
		id, expected, confirmed, phrase := fields(in)
		p, err := prepareWarehouseCategoryDelete(ctx, db, kind, id, expected, confirmed)
		if err != nil || !p.Ready {
			return p.response("needs_input"), source(id), p.Warnings, err
		}
		if !confirmed {
			return p.response("confirmation_required"), source(id), p.Warnings, nil
		}
		if strings.TrimSpace(phrase) != warehouseCategoryDeletePhrase(kind, id) {
			return p.response("elevated_confirmation_required"), source(id), append(p.Warnings, "No data was changed. Type the exact phrase from the preview."), nil
		}
		path := plural
		if kind == "third_category" {
			path = "subbiercategories"
		}
		var deleted map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/"+path+"/"+url.PathEscape(fmt.Sprint(id)), http.MethodDelete, map[string]any{"expected_updated_at": expected, "confirm_delete": true, "confirmation_text": warehouseCategoryDeletePhrase(kind, id)}, &deleted); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "deleted", "deletion": deleted, "diff": p.Diff}, source(id), p.Warnings, nil
	})
}

func warehouseCategoryDeletePhrase(kind string, id any) string {
	return "DELETE WAREHOUSE " + strings.ToUpper(kind) + " " + fmt.Sprint(id)
}

func prepareWarehouseCategoryDelete(ctx context.Context, db *store.Store, kind string, id any, expected string, confirmed bool) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	var query, usageQuery, table, idField string
	switch kind {
	case "category":
		table, idField = "categories", "category_id"
		query = `SELECT categoryid AS id,name,abbreviation,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM categories WHERE categoryid=$1`
		usageQuery = `SELECT (SELECT COUNT(*) FROM products WHERE categoryid=$1) AS product_count,(SELECT COUNT(*) FROM subcategories WHERE categoryid=$1) AS child_count`
	case "subcategory":
		table, idField = "subcategories", "subcategory_id"
		query = `SELECT subcategoryid AS id,name,abbreviation,categoryid AS category_id,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM subcategories WHERE subcategoryid=$1`
		usageQuery = `SELECT (SELECT COUNT(*) FROM products WHERE subcategoryid=$1) AS product_count,(SELECT COUNT(*) FROM subbiercategories WHERE subcategoryid=$1) AS child_count`
	case "third_category":
		table, idField = "subbiercategories", "third_category_id"
		query = `SELECT subbiercategoryid AS id,name,abbreviation,subcategoryid AS subcategory_id,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM subbiercategories WHERE subbiercategoryid=$1`
		usageQuery = `SELECT COUNT(*) AS product_count,0 AS child_count FROM products WHERE subbiercategoryid=$1`
	default:
		return p, fmt.Errorf("unsupported warehouse category level")
	}
	if kind == "category" && (numericID(id) <= 0 || numericID(id) > math.MaxInt32) || kind != "category" && (nullableText(id) == "" || len([]rune(nullableText(id))) > 50) {
		p.require(idField, "Welche gültige ungenutzte Kategorie-ID soll entfernt werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, query, id)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require(idField, "Kategorie wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	version := nullableText(p.Current["updated_at"])
	if confirmed && expected == "" || expected != "" && expected != version {
		p.require("expected_updated_at", "Die genaue Version aus einer neuen Löschvorschau ist erforderlich.", version)
	}
	usage, err := db.Query(ctx, usageQuery, id)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = usage
	if len(usage) != 1 {
		return p, fmt.Errorf("category dependency counts unavailable")
	}
	if numericID(usage[0]["product_count"]) > 0 || numericID(usage[0]["child_count"]) > 0 {
		p.require("active_dependencies", "Nur ungenutzte Kategorien ohne Produkte und Unterkategorien können entfernt werden.", usage[0])
	}
	extra, err := db.Query(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE contype='f' AND confrelid=to_regclass($1) AND conrelid NOT IN (to_regclass('products'),to_regclass('categories'),to_regclass('subcategories'),to_regclass('subbiercategories'))) AS has_additional_references`, table)
	if err != nil {
		return p, err
	}
	if len(extra) == 1 && extra[0]["has_additional_references"] == true {
		p.require("additional_references", "Zusätzliche Fremdschlüssel erfordern eine gesonderte Prüfung.", nil)
	}
	p.Draft = map[string]any{idField: id, "expected_updated_at": version, "confirmation_text_required": warehouseCategoryDeletePhrase(kind, id), "product_count": usage[0]["product_count"], "child_count": usage[0]["child_count"]}
	p.Diff = map[string]map[string]any{"record": {"before": p.Current, "after": nil}}
	p.Warnings = append(p.Warnings, "Permanent deletion cannot be undone through MCP. No children or product assignments will be deleted or reassigned; audit history is retained.")
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	p.finish()
	return p, nil
}

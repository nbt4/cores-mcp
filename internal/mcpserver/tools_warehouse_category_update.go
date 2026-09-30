package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCategoryUpdateInput struct {
	MutationControl
	CategoryID        int64   `json:"category_id,omitempty" jsonschema:"Exact top-level category ID; IDs remain immutable."`
	Name              *string `json:"name,omitempty" jsonschema:"Replacement name, 1-100 characters, globally unique."`
	Abbreviation      *string `json:"abbreviation,omitempty" jsonschema:"Required nonempty abbreviation, at most 10 characters."`
	AllowSimilar      bool    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar category names."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool    `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the complete diff and receiving explicit confirmation."`
}

type WarehouseSubcategoryUpdateInput struct {
	MutationControl
	SubcategoryID     string  `json:"subcategory_id,omitempty" jsonschema:"Exact second-level category ID; IDs remain immutable."`
	Name              *string `json:"name,omitempty" jsonschema:"Replacement name, 1-100 characters, unique within its parent."`
	Abbreviation      *string `json:"abbreviation,omitempty" jsonschema:"Replacement abbreviation, at most 10 characters; empty clears it."`
	CategoryID        *int64  `json:"category_id,omitempty" jsonschema:"Replacement existing top-level parent. Moving must preserve linked product and descendant assignments."`
	AllowSimilar      bool    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar category names."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool    `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the complete diff and receiving explicit confirmation."`
}

type WarehouseThirdCategoryUpdateInput struct {
	MutationControl
	ThirdCategoryID   string  `json:"third_category_id,omitempty" jsonschema:"Exact third-level ID, returned as subbiercategory_id by create; IDs remain immutable."`
	Name              *string `json:"name,omitempty" jsonschema:"Replacement name, 1-100 characters, unique within its parent."`
	Abbreviation      *string `json:"abbreviation,omitempty" jsonschema:"Replacement abbreviation, at most 10 characters; empty clears it."`
	SubcategoryID     *string `json:"subcategory_id,omitempty" jsonschema:"Replacement existing second-level parent with valid ancestry. Moving must preserve linked product assignments."`
	AllowSimilar      bool    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar category names."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool    `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the complete diff and receiving explicit confirmation."`
}

func registerWarehouseCategoryUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	registerWarehouseCategoryUpdatePair(server, cfg, api, "categories", "category", func(ctx context.Context, in WarehouseCategoryUpdateInput) (preparedMutation, error) {
		return prepareWarehouseCategoryUpdate(ctx, db, "category", in.CategoryID, in.Name, in.Abbreviation, nil, in.AllowSimilar, in.ExpectedUpdatedAt, in.ConfirmUpdate)
	}, func(in WarehouseCategoryUpdateInput) (string, bool) {
		return fmt.Sprint(in.CategoryID), in.ConfirmUpdate
	})
	registerWarehouseCategoryUpdatePair(server, cfg, api, "subcategories", "subcategory", func(ctx context.Context, in WarehouseSubcategoryUpdateInput) (preparedMutation, error) {
		return prepareWarehouseCategoryUpdate(ctx, db, "subcategory", strings.TrimSpace(in.SubcategoryID), in.Name, in.Abbreviation, in.CategoryID, in.AllowSimilar, in.ExpectedUpdatedAt, in.ConfirmUpdate)
	}, func(in WarehouseSubcategoryUpdateInput) (string, bool) {
		return strings.TrimSpace(in.SubcategoryID), in.ConfirmUpdate
	})
	registerWarehouseCategoryUpdatePair(server, cfg, api, "third_categories", "third_category", func(ctx context.Context, in WarehouseThirdCategoryUpdateInput) (preparedMutation, error) {
		return prepareWarehouseCategoryUpdate(ctx, db, "third_category", strings.TrimSpace(in.ThirdCategoryID), in.Name, in.Abbreviation, in.SubcategoryID, in.AllowSimilar, in.ExpectedUpdatedAt, in.ConfirmUpdate)
	}, func(in WarehouseThirdCategoryUpdateInput) (string, bool) {
		return strings.TrimSpace(in.ThirdCategoryID), in.ConfirmUpdate
	})
}

func registerWarehouseCategoryUpdatePair[In any](server *mcp.Server, cfg config.Config, api *coreAPIClient, plural, kind string, prepare func(context.Context, In) (preparedMutation, error), identity func(In) (string, bool)) {
	sources := func(id string, p preparedMutation) []Source {
		result := []Source{{Service: "warehousecore", Entity: kind, ID: id}}
		for _, row := range p.RelatedRecords {
			if entity, ok := row["entity"].(string); ok && row["id"] != nil {
				result = append(result, Source{Service: "warehousecore", Entity: entity, ID: fmt.Sprint(row["id"])})
			}
		}
		return result
	}
	addWritePreparationTool(server, "warehouse."+plural+".prepare_update", "Prepare warehouse "+kind+" update", "Load immutable identity, all editable fields, exact version and a complete diff. Check duplicate and similar names, ancestry and linked products before any parent change; show affected record counts.", func(ctx context.Context, in In) (any, []Source, []string, error) {
		p, err := prepare(ctx, in)
		id, _ := identity(in)
		return p.response("draft"), sources(id, p), p.Warnings, err
	})
	addUpdateTool(server, "warehouse."+plural+".update", "Update warehouse "+kind, "Update a category after full preview, exact version and explicit confirmation. IDs are immutable and conflicting product assignments block hierarchy moves. WarehouseCore commits the edit, audit and durable replay receipt atomically.", func(ctx context.Context, in In) (any, []Source, []string, error) {
		p, err := prepare(ctx, in)
		id, confirmed := identity(in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), sources(id, p), p.Warnings, err
		}
		if !confirmed {
			return p.response("confirmation_required"), sources(id, p), append(p.Warnings, "No data was changed. Present the complete diff and obtain explicit confirmation."), nil
		}
		path := plural
		if kind == "third_category" {
			path = "subbiercategories"
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/"+path+"/"+url.PathEscape(id), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", kind: updated, "diff": p.Diff}, sources(id, p), p.Warnings, nil
	})
}

func prepareWarehouseCategoryUpdate(ctx context.Context, db *store.Store, kind string, id any, name, abbreviation *string, parent any, allowSimilar bool, expected string, confirmed bool) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	var query, duplicateQuery, usageQuery, idField, parentField string
	switch kind {
	case "category":
		idField = "category_id"
		query = `SELECT categoryid AS id,name,abbreviation,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM categories WHERE categoryid=$1`
		duplicateQuery = `SELECT categoryid AS id,name FROM categories WHERE categoryid<>$1 AND lower(trim(name))=lower($2) LIMIT 10`
		usageQuery = `SELECT (SELECT COUNT(*) FROM products WHERE categoryid=$1 OR subcategoryid IN (SELECT subcategoryid FROM subcategories WHERE categoryid=$1) OR subbiercategoryid IN (SELECT t.subbiercategoryid FROM subbiercategories t JOIN subcategories s ON s.subcategoryid=t.subcategoryid WHERE s.categoryid=$1)) AS product_count,(SELECT COUNT(*) FROM subcategories WHERE categoryid=$1) AS subcategory_count,(SELECT COUNT(*) FROM subbiercategories t JOIN subcategories s ON s.subcategoryid=t.subcategoryid WHERE s.categoryid=$1) AS third_category_count`
	case "subcategory":
		idField, parentField = "subcategory_id", "category_id"
		query = `SELECT subcategoryid AS id,name,abbreviation,categoryid AS category_id,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM subcategories WHERE subcategoryid=$1`
		duplicateQuery = `SELECT subcategoryid AS id,name FROM subcategories WHERE subcategoryid<>$1 AND lower(trim(name))=lower($2) AND categoryid=$3 LIMIT 10`
		usageQuery = `SELECT (SELECT COUNT(*) FROM products WHERE subcategoryid=$1 OR subbiercategoryid IN (SELECT subbiercategoryid FROM subbiercategories WHERE subcategoryid=$1)) AS product_count,(SELECT COUNT(*) FROM subbiercategories WHERE subcategoryid=$1) AS third_category_count`
	case "third_category":
		idField, parentField = "third_category_id", "subcategory_id"
		query = `SELECT subbiercategoryid AS id,name,abbreviation,subcategoryid AS subcategory_id,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM subbiercategories WHERE subbiercategoryid=$1`
		duplicateQuery = `SELECT subbiercategoryid AS id,name FROM subbiercategories WHERE subbiercategoryid<>$1 AND lower(trim(name))=lower($2) AND subcategoryid=$3 LIMIT 10`
		usageQuery = `SELECT COUNT(*) AS product_count FROM products WHERE subbiercategoryid=$1`
	default:
		return p, fmt.Errorf("unsupported warehouse category level")
	}
	if kind == "category" && (numericID(id) <= 0 || numericID(id) > math.MaxInt32) || kind != "category" && (nullableText(id) == "" || len([]rune(nullableText(id))) > 50) {
		p.require(idField, "Welche gültige Kategorie-ID soll geändert werden?", nil)
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
	p.Draft["name"], p.Draft["abbreviation"] = p.Current["name"], p.Current["abbreviation"]
	if parentField != "" {
		p.Draft[parentField] = p.Current[parentField]
	}
	before := cloneMap(p.Draft)
	p.Draft["name"] = strings.TrimSpace(nullableText(p.Draft["name"]))
	if p.Draft["abbreviation"] != nil {
		p.Draft["abbreviation"] = strings.TrimSpace(nullableText(p.Draft["abbreviation"]))
	}
	if name != nil {
		p.Draft["name"] = strings.TrimSpace(*name)
	}
	if abbreviation != nil {
		p.Draft["abbreviation"] = strings.TrimSpace(*abbreviation)
	}
	switch value := parent.(type) {
	case *int64:
		if value != nil {
			p.Draft[parentField] = *value
		}
	case *string:
		if value != nil {
			p.Draft[parentField] = strings.TrimSpace(*value)
		}
	}
	if value := nullableText(p.Draft["name"]); value == "" || len([]rune(value)) > 100 {
		p.require("name", "Name benötigt 1-100 Zeichen.", nil)
	}
	if value := nullableText(p.Draft["abbreviation"]); len([]rune(value)) > 10 || kind == "category" && value == "" {
		p.require("abbreviation", "Abkürzung benötigt höchstens 10 Zeichen; auf Hauptebene ist sie verpflichtend.", nil)
	}
	if kind == "subcategory" && (numericID(p.Draft[parentField]) <= 0 || numericID(p.Draft[parentField]) > math.MaxInt32) || kind == "third_category" && (nullableText(p.Draft[parentField]) == "" || len([]rune(nullableText(p.Draft[parentField]))) > 50) {
		p.require(parentField, "Gültiger vorhandener Elternknoten erforderlich.", nil)
	}
	p.Diff = map[string]map[string]any{}
	for key, value := range before {
		if !reflect.DeepEqual(value, p.Draft[key]) {
			p.Diff[key] = map[string]any{"before": value, "after": p.Draft[key]}
		}
	}
	p.Draft["expected_updated_at"] = version
	if len(p.Diff) == 0 {
		p.require("changed_fields", "Welche Kategoriefelder sollen geändert werden?", nil)
	}
	if confirmed && expected == "" || expected != "" && expected != version {
		p.require("expected_updated_at", "Die genaue Version aus einer neuen Vorschau ist erforderlich.", version)
	}
	if len(p.Missing) > 0 {
		p.finish()
		return p, nil
	}
	args := []any{id, p.Draft["name"]}
	if parentField != "" {
		parentQuery := `SELECT categoryid AS id,name,categoryid AS category_id,'category'::text AS entity FROM categories WHERE categoryid=$1`
		if kind == "third_category" {
			parentQuery = `SELECT s.subcategoryid AS id,s.name,s.categoryid AS category_id,'subcategory'::text AS entity FROM subcategories s JOIN categories c ON c.categoryid=s.categoryid WHERE s.subcategoryid=$1`
		}
		parents, err := db.Query(ctx, parentQuery, p.Draft[parentField])
		if err != nil {
			return p, err
		}
		p.RelatedRecords = append(p.RelatedRecords, parents...)
		if len(parents) != 1 {
			p.require(parentField, "Elternknoten oder dessen Hauptkategorie wurde nicht gefunden.", nil)
			p.finish()
			return p, nil
		}
		args = append(args, p.Draft[parentField])
		if _, changed := p.Diff[parentField]; changed {
			dependencyQuery := `SELECT productid AS id,name,categoryid AS category_id,subcategoryid AS subcategory_id,subbiercategoryid AS third_category_id,'product'::text AS entity FROM products WHERE (subcategoryid=$1 OR subbiercategoryid IN (SELECT subbiercategoryid FROM subbiercategories WHERE subcategoryid=$1)) AND (categoryid IS DISTINCT FROM $2::int OR subcategoryid IS DISTINCT FROM $1) ORDER BY productid LIMIT 10`
			dependencyArgs := []any{id, parents[0]["category_id"]}
			if kind == "third_category" {
				dependencyQuery = `SELECT productid AS id,name,categoryid AS category_id,subcategoryid AS subcategory_id,'product'::text AS entity FROM products WHERE subbiercategoryid=$1 AND (subcategoryid IS DISTINCT FROM $2::varchar OR categoryid IS DISTINCT FROM $3::int) ORDER BY productid LIMIT 10`
				dependencyArgs = []any{id, p.Draft[parentField], parents[0]["category_id"]}
			}
			conflicts, err := db.Query(ctx, dependencyQuery, dependencyArgs...)
			if err != nil {
				return p, err
			}
			if len(conflicts) > 0 {
				p.RelatedRecords = append(p.RelatedRecords, conflicts...)
				p.require("linked_products", "Verschieben widerspricht bestehenden Produktzuordnungen. Zuordnungen zuerst prüfen und bearbeiten.", conflicts)
			}
		}
	}
	duplicates, err := db.Query(ctx, duplicateQuery, args...)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_category", "Name ist unter diesem Elternknoten bereits vorhanden.", duplicates)
	}
	usage, err := db.Query(ctx, usageQuery, id)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = append(p.RelatedRecords, usage...)
	if _, changed := p.Diff["name"]; changed {
		candidates, err := warehouseMasterCandidates(ctx, db, kind, nullableText(p.Draft["name"]), 20)
		if err != nil {
			return p, err
		}
		similar := []map[string]any{}
		for _, row := range candidates {
			if fmt.Sprint(row["id"]) != fmt.Sprint(id) {
				row["entity"] = kind
				similar = append(similar, row)
			}
		}
		p.RelatedRecords = append(p.RelatedRecords, similar...)
		if len(similar) > 0 && !allowSimilar {
			p.require("similar_category_review", "Ähnliche Kategorien prüfen und allow_similar nur nach Freigabe setzen.", similar)
		}
	}
	p.Warnings = append(p.Warnings, "Names and abbreviations are shared by linked records; category IDs remain unchanged.")
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	p.finish()
	return p, nil
}

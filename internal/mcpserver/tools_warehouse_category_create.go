package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCategoryCreateInput struct {
	MutationControl
	Name            string `json:"name,omitempty" jsonschema:"Top-level category name, 1-100 characters."`
	Abbreviation    string `json:"abbreviation,omitempty" jsonschema:"Required top-level abbreviation, at most 10 characters."`
	AllowSimilar    bool   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing categories."`
	ConfirmCreation bool   `json:"confirm_creation,omitempty"`
}

type WarehouseSubcategoryCreateInput struct {
	MutationControl
	Name            string `json:"name,omitempty" jsonschema:"Second-level category name, 1-100 characters."`
	Abbreviation    string `json:"abbreviation,omitempty" jsonschema:"Optional abbreviation, at most 10 characters."`
	CategoryID      int64  `json:"category_id,omitempty" jsonschema:"Exact existing top-level category ID."`
	AllowSimilar    bool   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing subcategories."`
	ConfirmCreation bool   `json:"confirm_creation,omitempty"`
}

type WarehouseThirdCategoryCreateInput struct {
	MutationControl
	Name            string `json:"name,omitempty" jsonschema:"Third-level category name, 1-100 characters."`
	Abbreviation    string `json:"abbreviation,omitempty" jsonschema:"Optional abbreviation, at most 10 characters."`
	SubcategoryID   string `json:"subcategory_id,omitempty" jsonschema:"Exact existing second-level category ID."`
	AllowSimilar    bool   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing third-level categories."`
	ConfirmCreation bool   `json:"confirm_creation,omitempty"`
}

func registerWarehouseCategoryCreateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.categories.prepare_create", "Prepare warehouse category", "Validate category name and abbreviation, detect exact and similar top-level categories, and show the standalone creation draft.", func(ctx context.Context, input WarehouseCategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "category", input.Name, input.Abbreviation, nil, input.AllowSimilar)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "category"}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.categories.create", "Create warehouse category", "Create one top-level category after duplicate review and explicit confirmation; WarehouseCore stores audit and durable idempotency atomically.", func(ctx context.Context, input WarehouseCategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "category", input.Name, input.Abbreviation, nil, input.AllowSimilar)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "category"}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "category"}}, []string{"No data was changed."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/categories", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "category": created}, []Source{{Service: "warehousecore", Entity: "category", ID: fmt.Sprint(created["category_id"])}}, nil, nil
	})
	addWritePreparationTool(server, "warehouse.subcategories.prepare_create", "Prepare warehouse subcategory", "Validate an existing parent category and detect duplicate or similar second-level category names.", func(ctx context.Context, input WarehouseSubcategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "subcategory", input.Name, input.Abbreviation, input.CategoryID, input.AllowSimilar)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "category", ID: fmt.Sprint(input.CategoryID)}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.subcategories.create", "Create warehouse subcategory", "Create one second-level category under an existing parent after duplicate review and confirmation, with target audit and durable idempotency.", func(ctx context.Context, input WarehouseSubcategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "subcategory", input.Name, input.Abbreviation, input.CategoryID, input.AllowSimilar)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "category", ID: fmt.Sprint(input.CategoryID)}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "category", ID: fmt.Sprint(input.CategoryID)}}, []string{"No data was changed."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/subcategories", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "subcategory": created}, []Source{{Service: "warehousecore", Entity: "category", ID: fmt.Sprint(input.CategoryID)}, {Service: "warehousecore", Entity: "subcategory", ID: fmt.Sprint(created["subcategory_id"])}}, nil, nil
	})
	addWritePreparationTool(server, "warehouse.third_categories.prepare_create", "Prepare warehouse third-level category", "Validate the parent subcategory and detect duplicate or similar third-level category names.", func(ctx context.Context, input WarehouseThirdCategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "third_category", input.Name, input.Abbreviation, input.SubcategoryID, input.AllowSimilar)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "subcategory", ID: input.SubcategoryID}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.third_categories.create", "Create warehouse third-level category", "Create one third-level category under an existing subcategory after duplicate review and confirmation, with target audit and durable idempotency.", func(ctx context.Context, input WarehouseThirdCategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseCategoryCreate(ctx, db, "third_category", input.Name, input.Abbreviation, input.SubcategoryID, input.AllowSimilar)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "subcategory", ID: input.SubcategoryID}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "subcategory", ID: input.SubcategoryID}}, []string{"No data was changed."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/subbiercategories", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "third_category": created}, []Source{{Service: "warehousecore", Entity: "subcategory", ID: input.SubcategoryID}, {Service: "warehousecore", Entity: "third_category", ID: fmt.Sprint(created["subbiercategory_id"])}}, nil, nil
	})
}

func prepareWarehouseCategoryCreate(ctx context.Context, db *store.Store, kind, rawName, rawAbbreviation string, parent any, allowSimilar bool) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	name, abbreviation := strings.TrimSpace(rawName), strings.TrimSpace(rawAbbreviation)
	if name == "" || len([]rune(name)) > 100 {
		p.require("name", "Welcher Kategoriename mit 1-100 Zeichen soll angelegt werden?", nil)
	}
	if len([]rune(abbreviation)) > 10 || kind == "category" && abbreviation == "" {
		p.require("abbreviation", "Welche Abkürzung mit höchstens 10 Zeichen gilt?", nil)
	}
	p.Draft = map[string]any{"name": name, "abbreviation": abbreviation}
	var parentField, parentQuery, duplicateQuery string
	switch kind {
	case "category":
		duplicateQuery = `SELECT categoryid AS id,name,abbreviation FROM categories WHERE lower(trim(name))=lower($1) LIMIT 10`
	case "subcategory":
		parentField = "category_id"
		if numericID(parent) <= 0 {
			p.require(parentField, "Welche vorhandene Hauptkategorie-ID gilt?", nil)
		}
		parentQuery = `SELECT categoryid AS id,name FROM categories WHERE categoryid=$1`
		duplicateQuery = `SELECT subcategoryid AS id,name,abbreviation,categoryid AS category_id FROM subcategories WHERE categoryid=$2 AND lower(trim(name))=lower($1) LIMIT 10`
	case "third_category":
		parentField = "subcategory_id"
		if strings.TrimSpace(fmt.Sprint(parent)) == "" {
			p.require(parentField, "Welche vorhandene Unterkategorie-ID gilt?", nil)
		}
		parentQuery = `SELECT subcategoryid AS id,name,categoryid AS category_id FROM subcategories WHERE subcategoryid=$1`
		duplicateQuery = `SELECT subbiercategoryid AS id,name,abbreviation,subcategoryid AS subcategory_id FROM subbiercategories WHERE subcategoryid=$2 AND lower(trim(name))=lower($1) LIMIT 10`
	default:
		return p, fmt.Errorf("unsupported warehouse category level")
	}
	if parentField != "" {
		p.Draft[parentField] = parent
	}
	if name == "" || len(p.Missing) > 0 {
		p.finish()
		return p, nil
	}
	if parentQuery != "" {
		parentRows, err := db.Query(ctx, parentQuery, parent)
		if err != nil {
			return p, err
		}
		if len(parentRows) != 1 {
			p.require(parentField, "Übergeordnete Kategorie wurde nicht gefunden.", nil)
			p.finish()
			return p, nil
		}
		p.RelatedRecords = append(p.RelatedRecords, parentRows[0])
	}
	var duplicates []map[string]any
	var err error
	if parentField == "" {
		duplicates, err = db.Query(ctx, duplicateQuery, name)
	} else {
		duplicates, err = db.Query(ctx, duplicateQuery, name, parent)
	}
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_category", "Kategorie existiert bereits unter diesem Elternknoten; vorhandene ID verwenden.", duplicates)
	}
	candidates, err := warehouseMasterCandidates(ctx, db, kind, name, 20)
	if err != nil {
		return p, err
	}
	similar := 0
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" && (parentField == "" || fmt.Sprint(candidate[parentField]) == fmt.Sprint(parent)) {
			continue
		}
		p.RelatedRecords = append(p.RelatedRecords, candidate)
		similar++
	}
	if similar > 0 && !allowSimilar {
		p.require("similar_category_review", "Ähnliche Kategorien prüfen und nur nach ausdrücklicher Freigabe allow_similar setzen.", candidates)
	}
	p.Diff = map[string]map[string]any{"name": {"before": nil, "after": name}, "abbreviation": {"before": nil, "after": abbreviation}}
	if parentField != "" {
		p.Diff[parentField] = map[string]any{"before": nil, "after": parent}
	}
	p.finish()
	return p, nil
}

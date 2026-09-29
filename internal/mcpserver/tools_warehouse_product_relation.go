package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseProductRelationInput struct {
	MutationControl
	ProductID           int64   `json:"product_id,omitempty" jsonschema:"Exact source WarehouseCore product ID."`
	DependencyProductID int64   `json:"dependency_product_id,omitempty" jsonschema:"Exact related WarehouseCore product ID; cannot equal product_id."`
	RelationType        string  `json:"relation_type,omitempty" jsonschema:"required, recommended, compatible, consumes, alternative or included. Empty preserves an existing type or defaults to recommended."`
	AssignmentScope     string  `json:"assignment_scope,omitempty" jsonschema:"product, device or case. Empty preserves an existing scope or defaults to product."`
	DefaultQuantity     float64 `json:"default_quantity,omitempty" jsonschema:"Positive finite quantity. Zero preserves an existing quantity or defaults to one."`
	Notes               *string `json:"notes,omitempty" jsonschema:"Optional relationship note; empty string clears an existing note. At most 500 characters."`
	ExpectedUpdatedAt   string  `json:"expected_updated_at,omitempty" jsonschema:"Exact source product version from prepare_link_relation."`
	ConfirmLink         bool    `json:"confirm_link,omitempty" jsonschema:"Confirm the full relationship diff after preview."`
	ConfirmationText    string  `json:"confirmation_text,omitempty" jsonschema:"Exact product-bound phrase from the preview."`
}

func registerWarehouseProductRelationTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.products.prepare_link_relation", "Prepare warehouse product relationship", "Check both active products, current relationship, full diff and exact source product version without changing data.", func(ctx context.Context, input WarehouseProductRelationInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseProductRelation(ctx, db, input)
		return p.response("draft"), warehouseRelationSources(input), p.Warnings, err
	})
	addUpdateTool(server, "warehouse.products.link_relation", "Link warehouse products", "Create or update one typed warehouse product relationship through WarehouseCore after full diff, exact version, elevated confirmation, audit and durable idempotency.", func(ctx context.Context, input WarehouseProductRelationInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseProductRelation(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), warehouseRelationSources(input), p.Warnings, err
		}
		if !input.ConfirmLink {
			return p.response("confirmation_required"), warehouseRelationSources(input), append(p.Warnings, "No data was changed."), nil
		}
		if strings.TrimSpace(input.ConfirmationText) != warehouseRelationPhrase(input) {
			return p.response("elevated_confirmation_required"), warehouseRelationSources(input), append(p.Warnings, "No data was changed. Type the phrase from the preview."), nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/products/%d/dependencies", input.ProductID), http.MethodPost, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "linked", "relationship": result, "diff": p.Diff}, warehouseRelationSources(input), p.Warnings, nil
	})
}

func warehouseRelationPhrase(input WarehouseProductRelationInput) string {
	if input.ProductID <= 0 || input.DependencyProductID <= 0 {
		return ""
	}
	return fmt.Sprintf("LINK WAREHOUSE PRODUCT %d TO %d", input.ProductID, input.DependencyProductID)
}

func warehouseRelationSources(input WarehouseProductRelationInput) []Source {
	return []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(input.ProductID)}, {Service: "warehousecore", Entity: "product", ID: fmt.Sprint(input.DependencyProductID)}}
}

func prepareWarehouseProductRelation(ctx context.Context, db *store.Store, input WarehouseProductRelationInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Warehouse administrator permission is required to link products")
	}
	if input.ProductID <= 0 || input.DependencyProductID <= 0 || input.ProductID == input.DependencyProductID {
		p.require("product_ids", "Zwei verschiedene gültige Warehouse-Produkt-IDs angeben.", nil)
		p.finish()
		return p, nil
	}
	products, err := db.Query(ctx, `SELECT productid AS product_id,name,product_code,lifecycle_status,
		to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at
		FROM products WHERE productid IN($1,$2) ORDER BY productid`, input.ProductID, input.DependencyProductID)
	if err != nil {
		return p, err
	}
	for _, row := range products {
		if numericID(row["product_id"]) == input.ProductID {
			p.Current = row
		} else {
			p.RelatedRecords = append(p.RelatedRecords, row)
		}
	}
	if p.Current == nil || len(p.RelatedRecords) != 1 {
		p.require("product_ids", "Quell- oder Zielprodukt wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	if p.Current["lifecycle_status"] != "active" || p.RelatedRecords[0]["lifecycle_status"] != "active" {
		p.require("lifecycle_status", "Nur aktive Produkte können verknüpft werden.", nil)
	}
	version := fmt.Sprint(p.Current["updated_at"])
	if input.ConfirmLink && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Exakte Produktversion aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Produkt wurde seit der Vorschau geändert.", version)
	}
	existing, err := db.Query(ctx, `SELECT id AS relation_id,relation_type,assignment_scope,default_quantity,notes
		FROM product_dependencies WHERE product_id=$1 AND dependency_product_id=$2`, input.ProductID, input.DependencyProductID)
	if err != nil {
		return p, err
	}
	var before map[string]any
	if len(existing) == 1 {
		before = existing[0]
		p.RelatedRecords = append(p.RelatedRecords, before)
	}
	relationType := strings.ToLower(strings.TrimSpace(input.RelationType))
	if relationType == "" && before != nil {
		relationType = nullableText(before["relation_type"])
	}
	if relationType == "" {
		relationType = "recommended"
	}
	if !containsString([]string{"required", "recommended", "compatible", "consumes", "alternative", "included"}, relationType) {
		p.require("relation_type", "Welche gültige Beziehungsart gilt?", []string{"required", "recommended", "compatible", "consumes", "alternative", "included"})
	}
	scope := strings.ToLower(strings.TrimSpace(input.AssignmentScope))
	if scope == "" && before != nil {
		scope = nullableText(before["assignment_scope"])
	}
	if scope == "" {
		scope = "product"
	}
	if !containsString([]string{"product", "device", "case"}, scope) {
		p.require("assignment_scope", "Welche gültige Zuordnungsebene gilt?", []string{"product", "device", "case"})
	}
	quantity := input.DefaultQuantity
	if quantity == 0 && before != nil {
		quantity = numericFloat(before["default_quantity"])
	}
	if quantity == 0 {
		quantity = 1
	}
	if quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		p.require("default_quantity", "Welche endliche positive Standardmenge gilt?", nil)
	}
	var notes *string
	if input.Notes != nil {
		value := strings.TrimSpace(*input.Notes)
		notes = &value
	} else if before != nil && before["notes"] != nil {
		value := nullableText(before["notes"])
		notes = &value
	}
	if notes != nil && len([]rune(*notes)) > 500 {
		p.require("notes", "Notiz auf höchstens 500 Zeichen kürzen.", nil)
	}
	p.Draft = map[string]any{"dependency_product_id": input.DependencyProductID, "relation_type": relationType, "assignment_scope": scope, "default_quantity": quantity, "notes": notes, "expectedUpdatedAt": version}
	p.Diff = make(map[string]map[string]any)
	noteText := ""
	if notes != nil {
		noteText = *notes
	}
	fields := map[string]any{"relation_type": relationType, "assignment_scope": scope, "default_quantity": quantity, "notes": noteText}
	for name, after := range fields {
		var old any
		if before != nil {
			old = before[name]
			if name == "notes" {
				old = nullableText(before[name])
			}
			if name == "default_quantity" {
				old = numericFloat(before[name])
			}
		}
		if before == nil || fmt.Sprint(old) != fmt.Sprint(after) {
			p.Diff[name] = map[string]any{"before": old, "after": after}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Beziehung ist bereits unverändert.", nil)
	}
	p.Draft["confirmation_text_required"] = warehouseRelationPhrase(input)
	p.finish()
	return p, nil
}

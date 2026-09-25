package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

// ProductUpdateInput deliberately names every mutable field. The target Core
// receives a complete record, while the MCP caller only supplies changes.
type ProductUpdateInput struct {
	MutationControl
	ProductID         int64           `json:"product_id,omitempty" jsonschema:"Existing ProcurementCore product ID."`
	SKU               *string         `json:"sku,omitempty" jsonschema:"Unique product SKU, at most 80 characters."`
	Name              *string         `json:"name,omitempty" jsonschema:"Product name, at most 240 characters."`
	Description       *string         `json:"description,omitempty"`
	CategoryID        *int64          `json:"category_id,omitempty" jsonschema:"Existing category ID; zero clears the category."`
	Unit              *string         `json:"unit,omitempty" jsonschema:"Unit, at most 30 characters."`
	Manufacturer      *string         `json:"manufacturer,omitempty" jsonschema:"Manufacturer, at most 180 characters."`
	Model             *string         `json:"model,omitempty" jsonschema:"Model, at most 180 characters."`
	Parameters        *map[string]any `json:"parameters,omitempty" jsonschema:"Complete replacement category parameter object."`
	Attributes        *map[string]any `json:"attributes,omitempty" jsonschema:"Complete replacement attribute object."`
	ReorderPoint      *float64        `json:"reorder_point,omitempty" jsonschema:"Non-negative reorder point."`
	TargetStock       *float64        `json:"target_stock,omitempty" jsonschema:"Non-negative target stock."`
	Active            *bool           `json:"active,omitempty" jsonschema:"False archives the product; true restores it."`
	AllowSimilarName  bool            `json:"allow_similar_name,omitempty" jsonschema:"Set only after reviewing similar names."`
	ExpectedUpdatedAt string          `json:"expected_updated_at,omitempty" jsonschema:"Exact version returned by prepare_update."`
	ConfirmUpdate     bool            `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the complete diff and receiving explicit confirmation."`
}

func registerProcurementProductUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.products.prepare_update", "Prepare procurement product update", "Load the complete product, validate changed fields and dependencies, and show an exact before/after diff and version. active=false archives; active=true restores.", func(ctx context.Context, input ProductUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareProcurementProductUpdate(ctx, db, input)
		return prepared.response("draft"), procurementProductUpdateSources(input.ProductID, prepared), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.products.update", "Update procurement product", "Update or archive one product after prepare_update, complete diff, exact version and explicit confirmation. Active orders and requisitions block archival.", func(ctx context.Context, input ProductUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareProcurementProductUpdate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), procurementProductUpdateSources(input.ProductID, prepared), append(prepared.Warnings, "No data was changed."), err
		}
		if !input.ConfirmUpdate {
			return prepared.response("confirmation_required"), procurementProductUpdateSources(input.ProductID, prepared), append(prepared.Warnings, "No data was changed. Show the diff and obtain explicit confirmation."), nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/products/%d", input.ProductID), http.MethodPut, prepared.Draft, &updated); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "updated", "product": updated, "diff": prepared.Diff}, procurementProductUpdateSources(input.ProductID, prepared), prepared.Warnings, nil
	})
}

func procurementProductUpdateSources(id int64, prepared preparedMutation) []Source {
	sources := []Source{{Service: "procurementcore", Entity: "product", ID: fmt.Sprint(id)}}
	for _, row := range prepared.RelatedRecords {
		if orderID := numericID(row["order_id"]); orderID > 0 {
			sources = append(sources, Source{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(orderID)})
		}
		if requisitionID := numericID(row["requisition_id"]); requisitionID > 0 {
			sources = append(sources, Source{Service: "procurementcore", Entity: "requisition", ID: fmt.Sprint(requisitionID)})
		}
	}
	return sources
}

func prepareProcurementProductUpdate(ctx context.Context, db *store.Store, input ProductUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.ProductID <= 0 {
		p.require("product_id", "Welche gültige Produkt-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to update product details")
	}
	rows, err := db.Query(ctx, `SELECT id,sku,name,description,category_id,unit,manufacturer,model,parameters,attributes,active,reorder_point,target_stock,
		to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_products WHERE id=$1`, input.ProductID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("product_id", "Das Produkt wurde nicht gefunden. Welche gültige ID soll verwendet werden?", nil)
		p.finish()
		return p, nil
	}
	row := rows[0]
	version := rfc3339Value(row["updated_at"])
	p.Draft = map[string]any{
		"sku": nullableText(row["sku"]), "name": nullableText(row["name"]), "description": nullableText(row["description"]),
		"categoryId": row["category_id"], "unit": nullableText(row["unit"]), "manufacturer": nullableText(row["manufacturer"]),
		"model": nullableText(row["model"]), "parameters": productJSONMap(row["parameters"]), "attributes": productJSONMap(row["attributes"]),
		"active": row["active"], "reorderPoint": numericFloat(row["reorder_point"]), "targetStock": numericFloat(row["target_stock"]),
		"expectedUpdatedAt": version,
	}
	p.Current = cloneMap(p.Draft)
	setString := func(key string, value *string) {
		if value != nil {
			p.Draft[key] = strings.TrimSpace(*value)
		}
	}
	setString("sku", input.SKU)
	if input.SKU != nil {
		p.Draft["sku"] = strings.ToUpper(fmt.Sprint(p.Draft["sku"]))
	}
	setString("name", input.Name)
	setString("description", input.Description)
	setString("unit", input.Unit)
	setString("manufacturer", input.Manufacturer)
	setString("model", input.Model)
	if input.CategoryID != nil {
		if *input.CategoryID <= 0 {
			p.Draft["categoryId"] = nil
		} else {
			p.Draft["categoryId"] = *input.CategoryID
		}
	}
	if input.Parameters != nil {
		p.Draft["parameters"] = cloneMap(*input.Parameters)
	}
	if input.Attributes != nil {
		p.Draft["attributes"] = cloneMap(*input.Attributes)
	}
	if input.ReorderPoint != nil {
		p.Draft["reorderPoint"] = *input.ReorderPoint
	}
	if input.TargetStock != nil {
		p.Draft["targetStock"] = *input.TargetStock
	}
	if input.Active != nil {
		p.Draft["active"] = *input.Active
	}
	p.Diff = map[string]map[string]any{}
	for key, old := range p.Current {
		if key != "expectedUpdatedAt" && !reflect.DeepEqual(old, p.Draft[key]) {
			p.Diff[key] = map[string]any{"before": old, "after": p.Draft[key]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Produktfelder sollen geändert werden?", nil)
	}
	for _, field := range []struct {
		key string
		max int
	}{{"sku", 80}, {"name", 240}, {"unit", 30}, {"manufacturer", 180}, {"model", 180}} {
		value := fmt.Sprint(p.Draft[field.key])
		if (field.key == "sku" || field.key == "name") && value == "" || len([]rune(value)) > field.max {
			p.require(field.key, fmt.Sprintf("%s muss ausgefüllt sein und höchstens %d Zeichen enthalten.", field.key, field.max), nil)
		}
	}
	for _, field := range []string{"reorderPoint", "targetStock"} {
		value := numericFloat(p.Draft[field])
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			p.require(field, "Bestandsgrenzen müssen endliche, nicht-negative Zahlen sein.", nil)
		}
	}
	if id := numericID(p.Draft["categoryId"]); id > 0 {
		categories, queryErr := db.Query(ctx, `SELECT id,name,parameter_schema FROM proc_categories WHERE id=$1`, id)
		if queryErr != nil {
			return p, queryErr
		}
		if len(categories) != 1 {
			p.require("category_id", "Die Kategorie existiert nicht. Welche gültige Kategorie soll verwendet werden?", nil)
		} else {
			p.RelatedRecords = append(p.RelatedRecords, map[string]any{"category_id": id, "name": categories[0]["name"]})
		}
	}
	if input.SKU != nil || input.Name != nil {
		duplicates, queryErr := db.Query(ctx, `SELECT id,sku,name FROM proc_products WHERE id<>$1 AND (upper(sku)=upper($2) OR lower(name)=lower($3)) ORDER BY name LIMIT 25`, input.ProductID, p.Draft["sku"], p.Draft["name"])
		if queryErr != nil {
			return p, queryErr
		}
		for _, candidate := range duplicates {
			p.RelatedRecords = append(p.RelatedRecords, candidate)
			if strings.EqualFold(fmt.Sprint(candidate["sku"]), fmt.Sprint(p.Draft["sku"])) {
				p.require("duplicate_sku", "Diese SKU ist bereits einem anderen Produkt zugeordnet.", candidate)
			} else if !input.AllowSimilarName {
				p.require("similar_name", "Ein gleichnamiges Produkt existiert bereits. Ist diese Änderung dennoch gewollt?", candidate)
			}
		}
	}
	if p.Current["active"] == true && p.Draft["active"] == false {
		orders, queryErr := db.Query(ctx, `SELECT po.id AS order_id,po.number,po.status FROM proc_purchase_order_lines pol JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id WHERE pol.product_id=$1 AND po.status NOT IN ('cancelled','received') ORDER BY po.id LIMIT 50`, input.ProductID)
		if queryErr != nil {
			return p, queryErr
		}
		requisitions, queryErr := db.Query(ctx, `SELECT r.id AS requisition_id,r.number,r.status FROM proc_requisition_lines rl JOIN proc_requisitions r ON r.id=rl.requisition_id WHERE rl.product_id=$1 AND r.status IN ('draft','submitted','approved') ORDER BY r.id LIMIT 50`, input.ProductID)
		if queryErr != nil {
			return p, queryErr
		}
		p.RelatedRecords = append(p.RelatedRecords, orders...)
		p.RelatedRecords = append(p.RelatedRecords, requisitions...)
		if len(orders)+len(requisitions) > 0 {
			p.require("active_references", "Offene Bestellungen oder Bedarfe müssen vor der Archivierung abgeschlossen werden.", p.RelatedRecords)
		}
	}
	if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die exakte Version aus der aktuellen Vorschau muss übernommen werden.", version)
	} else if input.ExpectedUpdatedAt != "" && strings.TrimSpace(input.ExpectedUpdatedAt) != version {
		p.require("expected_updated_at", "Das Produkt wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", version)
	}
	p.finish()
	return p, nil
}

func productJSONMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return cloneMap(result)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil || result == nil {
		return map[string]any{}
	}
	return result
}

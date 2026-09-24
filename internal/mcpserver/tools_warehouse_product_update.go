package mcpserver

import (
	"context"
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

type WarehouseProductUpdateInput struct {
	MutationControl
	ProductID              int64          `json:"product_id,omitempty" jsonschema:"Exact WarehouseCore product ID."`
	Name                   *string        `json:"name,omitempty" jsonschema:"Optional replacement name; unique and at most 255 characters."`
	Description            *string        `json:"description,omitempty" jsonschema:"Optional replacement description; empty string clears it."`
	CategoryID             *int64         `json:"category_id,omitempty" jsonschema:"Optional existing top-level category ID."`
	SubcategoryID          *string        `json:"subcategory_id,omitempty" jsonschema:"Optional existing second-level category ID."`
	ThirdCategoryID        *string        `json:"third_category_id,omitempty" jsonschema:"Optional existing third-level category ID."`
	ManufacturerID         *int64         `json:"manufacturer_id,omitempty" jsonschema:"Optional existing manufacturer ID."`
	BrandID                *int64         `json:"brand_id,omitempty" jsonschema:"Optional existing brand ID belonging to the manufacturer."`
	ProductType            *string        `json:"product_type,omitempty" jsonschema:"Equipment, accessory or consumable."`
	TrackingMode           *string        `json:"tracking_mode,omitempty" jsonschema:"Individual, quantity or none. Existing devices and stock can block changes."`
	ProductKind            *string        `json:"product_kind,omitempty" jsonschema:"Standard, cable, consumable, container or service."`
	CountTypeID            *int64         `json:"count_type_id,omitempty" jsonschema:"Existing measurement unit; required for quantity tracking."`
	ModelNumber            *string        `json:"model_number,omitempty"`
	ManufacturerPartNumber *string        `json:"manufacturer_part_number,omitempty"`
	EAN                    *string        `json:"ean,omitempty"`
	GenericBarcode         *string        `json:"generic_barcode,omitempty" jsonschema:"Unique product barcode; empty string clears it."`
	MaintenanceInterval    *int           `json:"maintenance_interval,omitempty" jsonschema:"Non-negative maintenance interval."`
	ItemCostPerDay         *float64       `json:"item_cost_per_day,omitempty" jsonschema:"Non-negative daily price."`
	Weight                 *float64       `json:"weight,omitempty" jsonschema:"Non-negative weight in kilograms."`
	Height                 *float64       `json:"height,omitempty" jsonschema:"Non-negative height in centimeters."`
	Width                  *float64       `json:"width,omitempty" jsonschema:"Non-negative width in centimeters."`
	Depth                  *float64       `json:"depth,omitempty" jsonschema:"Non-negative depth in centimeters."`
	PowerConsumption       *float64       `json:"power_consumption,omitempty" jsonschema:"Non-negative power consumption in watts."`
	PositionInCategory     *int           `json:"position_in_category,omitempty" jsonschema:"Positive display position."`
	StockQuantity          *float64       `json:"stock_quantity,omitempty" jsonschema:"Quantity-tracked stock; distributed stock must be adjusted by a movement instead."`
	MinimumStockLevel      *float64       `json:"minimum_stock_level,omitempty" jsonschema:"Non-negative replenishment threshold."`
	PricePerUnit           *float64       `json:"price_per_unit,omitempty" jsonschema:"Non-negative unit price."`
	Attributes             map[string]any `json:"attributes,omitempty" jsonschema:"Complete replacement map of structured technical attributes. Empty map clears it."`
	ClearFields            []string       `json:"clear_fields,omitempty" jsonschema:"Optional nullable fields to clear: category_id, subcategory_id, third_category_id, manufacturer_id, brand_id, count_type_id, model_number, manufacturer_part_number, ean, maintenance_interval, item_cost_per_day, weight, height, width, depth, power_consumption, position_in_category, stock_quantity, minimum_stock_level, price_per_unit."`
	AllowSimilarProduct    bool           `json:"allow_similar_product,omitempty" jsonschema:"Set true only after reviewing similar existing products."`
	ExpectedUpdatedAt      string         `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate          bool           `json:"confirm_update,omitempty" jsonschema:"Set true only after showing the full before/after diff and receiving explicit confirmation."`
}

var warehouseProductUpdateFields = []string{
	"name", "description", "category_id", "subcategory_id", "subbiercategory_id", "manufacturer_id", "brand_id",
	"maintenance_interval", "item_cost_per_day", "weight", "height", "width", "depth", "power_consumption",
	"pos_in_category", "count_type_id", "stock_quantity", "min_stock_level", "generic_barcode", "price_per_unit",
	"product_type", "tracking_mode", "product_kind", "model_number", "manufacturer_part_number", "ean", "attributes",
}

var warehouseProductClearableFields = map[string]bool{
	"category_id": true, "subcategory_id": true, "subbiercategory_id": true, "manufacturer_id": true, "brand_id": true,
	"count_type_id": true, "model_number": true, "manufacturer_part_number": true, "ean": true,
	"maintenance_interval": true, "item_cost_per_day": true, "weight": true, "height": true, "width": true,
	"depth": true, "power_consumption": true, "pos_in_category": true, "stock_quantity": true,
	"min_stock_level": true, "price_per_unit": true,
}

var warehouseProductClearFieldAliases = map[string]string{
	"third_category_id":    "subbiercategory_id",
	"position_in_category": "pos_in_category",
	"minimum_stock_level":  "min_stock_level",
}

func registerWarehouseProductUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.products.prepare_update", "Prepare warehouse product update", "Load every editable product field for a Warehouse administrator, validate the proposed changes and related IDs, detect duplicates, and show a complete diff and exact version without changing data.", func(ctx context.Context, input WarehouseProductUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseProductUpdate(ctx, db, input)
		return p.response("draft"), warehouseProductUpdateSources(input.ProductID, p.RelatedRecords), untrustedTextWarning(), err
	})
	addUpdateTool(server, "warehouse.products.update", "Update warehouse product", "Update one WarehouseCore product after prepare_update, complete diff, unchanged version and explicit confirmation. WarehouseCore commits the update, audit and durable idempotency receipt together.", func(ctx context.Context, input WarehouseProductUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseProductUpdate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), warehouseProductUpdateSources(input.ProductID, p.RelatedRecords), []string{"No data was changed."}, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), warehouseProductUpdateSources(input.ProductID, p.RelatedRecords), []string{"No data was changed. Show the complete diff and obtain explicit confirmation."}, nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/products/%d", input.ProductID), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "product": updated, "diff": p.Diff}, []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(input.ProductID)}}, nil, nil
	})
}

func warehouseProductUpdateSources(id int64, related []map[string]any) []Source {
	result := []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(id)}}
	return append(result, sourcesFor("warehousecore", "product", related)...)
}

func prepareWarehouseProductUpdate(ctx context.Context, db *store.Store, input WarehouseProductUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.ProductID <= 0 {
		p.require("product_id", "Welche gültige Warehouse-Produkt-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Warehouse administrator permission is required to read and update product details")
	}
	rows, err := db.Query(ctx, `SELECT productid AS product_id,name,description,categoryid AS category_id,subcategoryid AS subcategory_id,
		subbiercategoryid AS subbiercategory_id,manufacturerid AS manufacturer_id,brandid AS brand_id,
		maintenanceinterval AS maintenance_interval,itemcostperday AS item_cost_per_day,weight,height,width,depth,
		powerconsumption AS power_consumption,pos_in_category,count_type_id,stock_quantity,min_stock_level,
		generic_barcode,price_per_unit,product_type,tracking_mode,lifecycle_status,product_code,product_kind,
		model_number,manufacturer_part_number,ean,attributes,
		to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at
		FROM products WHERE productid=$1`, input.ProductID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("product_id", "Das Produkt wurde nicht gefunden. Welche gültige ID soll verwendet werden?", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	version := fmt.Sprint(p.Current["updated_at"])
	for _, field := range warehouseProductUpdateFields {
		p.Draft[field] = p.Current[field]
	}
	p.Draft["expectedUpdatedAt"] = version
	before := cloneMap(p.Draft)
	setText := func(field string, value *string) {
		if value != nil {
			p.Draft[field] = strings.TrimSpace(*value)
		}
	}
	setID := func(field string, value *int64) {
		if value != nil {
			p.Draft[field] = *value
		}
	}
	setInt := func(field string, value *int) {
		if value != nil {
			p.Draft[field] = *value
		}
	}
	setFloat := func(field string, value *float64) {
		if value != nil {
			p.Draft[field] = *value
		}
	}
	setText("name", input.Name)
	setText("description", input.Description)
	setID("category_id", input.CategoryID)
	setText("subcategory_id", input.SubcategoryID)
	setText("subbiercategory_id", input.ThirdCategoryID)
	setID("manufacturer_id", input.ManufacturerID)
	setID("brand_id", input.BrandID)
	setText("product_type", input.ProductType)
	setText("tracking_mode", input.TrackingMode)
	setText("product_kind", input.ProductKind)
	setID("count_type_id", input.CountTypeID)
	setText("model_number", input.ModelNumber)
	setText("manufacturer_part_number", input.ManufacturerPartNumber)
	setText("ean", input.EAN)
	setText("generic_barcode", input.GenericBarcode)
	setInt("maintenance_interval", input.MaintenanceInterval)
	setFloat("item_cost_per_day", input.ItemCostPerDay)
	setFloat("weight", input.Weight)
	setFloat("height", input.Height)
	setFloat("width", input.Width)
	setFloat("depth", input.Depth)
	setFloat("power_consumption", input.PowerConsumption)
	setInt("pos_in_category", input.PositionInCategory)
	setFloat("stock_quantity", input.StockQuantity)
	setFloat("min_stock_level", input.MinimumStockLevel)
	setFloat("price_per_unit", input.PricePerUnit)
	if input.Attributes != nil {
		p.Draft["attributes"] = input.Attributes
	}
	for _, field := range input.ClearFields {
		if alias := warehouseProductClearFieldAliases[field]; alias != "" {
			field = alias
		}
		if !warehouseProductClearableFields[field] {
			p.require("clear_fields", "Welche unterstützten optionalen Felder sollen geleert werden?", sortedMapKeys(warehouseProductClearableFields))
			continue
		}
		p.Draft[field] = nil
	}
	p.Diff = map[string]map[string]any{}
	for _, field := range warehouseProductUpdateFields {
		if !reflect.DeepEqual(before[field], p.Draft[field]) {
			p.Diff[field] = map[string]any{"before": before[field], "after": p.Draft[field]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Produktfelder sollen tatsächlich geändert werden?", nil)
	}
	if p.Current["lifecycle_status"] != "active" {
		p.require("lifecycle_status", "Das Produkt ist archiviert. Es muss vor einer Bearbeitung reaktiviert werden.", p.Current["lifecycle_status"])
	}
	if err := validateWarehouseProductUpdateDraft(&p); err != nil {
		return p, err
	}
	if err := checkWarehouseProductUpdateRelations(ctx, db, input, &p); err != nil {
		return p, err
	}
	if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die genaue Version aus der aktuellen Vorschau muss übernommen werden.", version)
	} else if input.ExpectedUpdatedAt != "" && strings.TrimSpace(input.ExpectedUpdatedAt) != version {
		p.require("expected_updated_at", "Das Produkt wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", version)
	}
	if input.Name != nil || input.GenericBarcode != nil {
		if err := checkWarehouseProductUpdateDuplicates(ctx, db, input, &p); err != nil {
			return p, err
		}
	}
	if input.TrackingMode != nil {
		if err := checkWarehouseTrackingChange(ctx, db, input.ProductID, &p); err != nil {
			return p, err
		}
	}
	p.finish()
	return p, nil
}

func validateWarehouseProductUpdateDraft(p *preparedMutation) error {
	draft := p.Draft
	name := strings.TrimSpace(nullableText(draft["name"]))
	if name == "" || len([]rune(name)) > 255 {
		p.require("name", "Welcher eindeutige Produktname mit höchstens 255 Zeichen soll gespeichert werden?", nil)
	}
	productType := nullableText(draft["product_type"])
	tracking := nullableText(draft["tracking_mode"])
	if !containsString([]string{"equipment", "accessory", "consumable"}, productType) {
		p.require("product_type", "Welche Produktart gilt: equipment, accessory oder consumable?", nil)
	}
	if !containsString([]string{"individual", "quantity", "none"}, tracking) || productType == "consumable" && tracking != "quantity" || productType == "equipment" && tracking == "quantity" {
		p.require("tracking_mode", "Welche zulässige Trackingart gilt für die Produktart?", nil)
	}
	if !containsString([]string{"standard", "cable", "consumable", "container", "service"}, nullableText(draft["product_kind"])) {
		p.require("product_kind", "Welche gültige Produktklasse soll gespeichert werden?", nil)
	}
	if tracking == "quantity" && numericID(draft["count_type_id"]) <= 0 {
		p.require("count_type_id", "Welche vorhandene Maßeinheit gilt für den Mengenbestand?", nil)
	}
	for _, field := range []string{"maintenance_interval", "pos_in_category"} {
		if draft[field] != nil && (numericID(draft[field]) < 0 || field == "pos_in_category" && numericID(draft[field]) == 0) {
			p.require(field, "Bitte einen gültigen nichtnegativen Wert angeben.", nil)
		}
	}
	for _, field := range []string{"item_cost_per_day", "weight", "height", "width", "depth", "power_consumption", "stock_quantity", "min_stock_level", "price_per_unit"} {
		if draft[field] != nil {
			value := numericFloat(draft[field])
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				p.require(field, "Bitte einen endlichen nichtnegativen Wert angeben.", nil)
			}
		}
	}
	for _, field := range []string{"category_id", "manufacturer_id", "brand_id", "count_type_id"} {
		if draft[field] != nil && numericID(draft[field]) <= 0 {
			p.require(field, "Welche gültige vorhandene ID soll verwendet werden?", nil)
		}
	}
	return nil
}

func checkWarehouseProductUpdateRelations(ctx context.Context, db *store.Store, input WarehouseProductUpdateInput, p *preparedMutation) error {
	for _, relation := range []struct {
		changed bool
		field   string
		query   string
	}{
		{input.CategoryID != nil, "category_id", `SELECT categoryid AS id FROM categories WHERE categoryid=$1`},
		{input.ManufacturerID != nil, "manufacturer_id", `SELECT manufacturerid AS id FROM manufacturer WHERE manufacturerid=$1`},
		{input.CountTypeID != nil, "count_type_id", `SELECT count_type_id AS id FROM count_types WHERE count_type_id=$1`},
	} {
		if !relation.changed || p.Draft[relation.field] == nil {
			continue
		}
		rows, err := db.Query(ctx, relation.query, numericID(p.Draft[relation.field]))
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			p.require(relation.field, "Diese Stammdaten-ID existiert nicht. Welche vorhandene ID soll verwendet werden?", nil)
		}
	}
	if (input.BrandID != nil || input.ManufacturerID != nil || containsString(input.ClearFields, "manufacturer_id")) && p.Draft["brand_id"] != nil {
		rows, err := db.Query(ctx, `SELECT brandid AS id,manufacturerid AS manufacturer_id FROM brands WHERE brandid=$1`, numericID(p.Draft["brand_id"]))
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			p.require("brand_id", "Diese Marke existiert nicht. Welche vorhandene Marke soll verwendet werden?", nil)
		} else if rows[0]["manufacturer_id"] != nil && numericID(rows[0]["manufacturer_id"]) != numericID(p.Draft["manufacturer_id"]) {
			p.require("manufacturer_id", "Die Marke gehört zu einem anderen Hersteller. Bitte Hersteller und Marke gemeinsam passend wählen.", rows[0])
		}
	}
	if (input.SubcategoryID != nil || input.CategoryID != nil) && p.Draft["subcategory_id"] != nil {
		rows, err := db.Query(ctx, `SELECT subcategoryid AS id,categoryid AS category_id FROM subcategories WHERE subcategoryid=$1`, p.Draft["subcategory_id"])
		if err != nil {
			return err
		}
		if len(rows) != 1 || numericID(rows[0]["category_id"]) != numericID(p.Draft["category_id"]) {
			p.require("subcategory_id", "Die Unterkategorie fehlt oder gehört zu einer anderen Hauptkategorie.", rows)
		}
	}
	if (input.ThirdCategoryID != nil || input.SubcategoryID != nil) && p.Draft["subbiercategory_id"] != nil {
		rows, err := db.Query(ctx, `SELECT subbiercategoryid AS id,subcategoryid AS subcategory_id FROM subbiercategories WHERE subbiercategoryid=$1`, p.Draft["subbiercategory_id"])
		if err != nil {
			return err
		}
		if len(rows) != 1 || fmt.Sprint(rows[0]["subcategory_id"]) != fmt.Sprint(p.Draft["subcategory_id"]) {
			p.require("third_category_id", "Die dritte Kategorieebene fehlt oder gehört zu einer anderen Unterkategorie.", rows)
		}
	}
	return nil
}

func checkWarehouseProductUpdateDuplicates(ctx context.Context, db *store.Store, input WarehouseProductUpdateInput, p *preparedMutation) error {
	name := nullableText(p.Draft["name"])
	barcode := nullableText(p.Draft["generic_barcode"])
	first := ""
	if words := strings.Fields(name); len(words) > 0 {
		first = words[0]
	}
	if first == "" {
		return nil
	}
	rows, err := db.Query(ctx, `SELECT productid AS id,name,generic_barcode FROM products
		WHERE productid<>$1 AND (lower(trim(name))=lower(trim($2)) OR ($3<>'' AND lower(trim(generic_barcode))=lower(trim($3))) OR position(lower($4) in lower(name))>0)
		ORDER BY CASE WHEN lower(trim(name))=lower(trim($2)) THEN 0 ELSE 1 END,name LIMIT 100`, input.ProductID, name, barcode, first)
	if err != nil {
		return err
	}
	for _, row := range rows {
		candidateName := nullableText(row["name"])
		candidateBarcode := nullableText(row["generic_barcode"])
		if strings.EqualFold(strings.TrimSpace(candidateName), strings.TrimSpace(name)) || barcode != "" && strings.EqualFold(candidateBarcode, barcode) {
			p.RelatedRecords = append(p.RelatedRecords, row)
			p.require("duplicate_product", "Ein anderes Produkt hat bereits diesen Namen oder Barcode.", row)
		} else if warehouseMatchScore(name, candidateName, "") >= 50 {
			p.RelatedRecords = append(p.RelatedRecords, row)
			if !input.AllowSimilarProduct {
				p.require("similar_product_review", "Ist dieses Produkt trotz ähnlichem Bestandsnamen eindeutig der bestehende Datensatz mit dieser ID?", row)
			}
		}
	}
	if len(rows) >= 100 {
		p.require("product_search_limit", "Die Ähnlichkeitssuche ist unvollständig. Bitte den Produktbestand gezielt prüfen.", nil)
	}
	return nil
}

func checkWarehouseTrackingChange(ctx context.Context, db *store.Store, id int64, p *preparedMutation) error {
	oldMode := nullableText(p.Current["tracking_mode"])
	newMode := nullableText(p.Draft["tracking_mode"])
	if oldMode == newMode {
		return nil
	}
	if oldMode == "individual" && newMode != "individual" {
		rows, err := db.Query(ctx, `SELECT count(*) AS device_count FROM devices WHERE productid=$1`, id)
		if err != nil {
			return err
		}
		if len(rows) > 0 && numericID(rows[0]["device_count"]) > 0 {
			p.require("tracking_mode", "Produkte mit Geräten müssen individuell verfolgt bleiben.", rows[0])
		}
	}
	if oldMode == "quantity" && newMode != "quantity" {
		rows, err := db.Query(ctx, `SELECT COALESCE(sum(quantity),0) AS location_stock FROM product_locations WHERE product_id=$1`, id)
		if err != nil {
			return err
		}
		if len(rows) > 0 && numericFloat(rows[0]["location_stock"]) > 0 {
			p.require("tracking_mode", "Mengenprodukte mit Bestand dürfen die Trackingart nicht wechseln.", rows[0])
		}
	}
	return nil
}

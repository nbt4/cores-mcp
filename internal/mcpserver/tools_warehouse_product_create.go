package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseProductCreateInput struct {
	MutationControl
	Name                      string         `json:"name,omitempty" jsonschema:"Required unique human-readable product name."`
	Description               string         `json:"description,omitempty" jsonschema:"Recommended description of purpose and use."`
	ProductType               string         `json:"product_type,omitempty" jsonschema:"One of equipment, accessory, or consumable; defaults to equipment."`
	TrackingMode              string         `json:"tracking_mode,omitempty" jsonschema:"One of individual, quantity, or none; defaults according to product_type."`
	ProductKind               string         `json:"product_kind,omitempty" jsonschema:"One of standard, cable, consumable, container, or service."`
	ManufacturerID            int64          `json:"manufacturer_id,omitempty" jsonschema:"Exact existing manufacturer ID."`
	ManufacturerName          string         `json:"manufacturer_name,omitempty" jsonschema:"Manufacturer name to resolve or propose for creation."`
	ManufacturerWebsite       string         `json:"manufacturer_website,omitempty" jsonschema:"Optional website used only when creating the manufacturer."`
	CreateManufacturer        bool           `json:"create_manufacturer,omitempty" jsonschema:"Set true only after deciding that no candidate is the intended manufacturer."`
	BrandID                   int64          `json:"brand_id,omitempty" jsonschema:"Exact existing brand ID."`
	BrandName                 string         `json:"brand_name,omitempty" jsonschema:"Optional brand or product-line name to resolve or propose for creation."`
	CreateBrand               bool           `json:"create_brand,omitempty" jsonschema:"Set true only after deciding that no candidate is the intended brand."`
	CategoryID                int64          `json:"category_id,omitempty" jsonschema:"Exact existing top-level category ID."`
	CategoryName              string         `json:"category_name,omitempty" jsonschema:"Top-level category name to resolve or propose for creation."`
	CategoryAbbreviation      string         `json:"category_abbreviation,omitempty" jsonschema:"Required short code when creating a top-level category."`
	CreateCategory            bool           `json:"create_category,omitempty" jsonschema:"Set true only after deciding that no candidate is the intended category."`
	SubcategoryID             string         `json:"subcategory_id,omitempty" jsonschema:"Exact existing second-level category ID."`
	SubcategoryName           string         `json:"subcategory_name,omitempty" jsonschema:"Optional second-level category name to resolve or propose for creation."`
	SubcategoryAbbreviation   string         `json:"subcategory_abbreviation,omitempty"`
	CreateSubcategory         bool           `json:"create_subcategory,omitempty" jsonschema:"Set true only after deciding that no candidate is the intended subcategory."`
	ThirdCategoryID           string         `json:"third_category_id,omitempty" jsonschema:"Exact existing third-level category ID."`
	ThirdCategoryName         string         `json:"third_category_name,omitempty" jsonschema:"Optional third-level category name to resolve or propose for creation."`
	ThirdCategoryAbbreviation string         `json:"third_category_abbreviation,omitempty"`
	CreateThirdCategory       bool           `json:"create_third_category,omitempty" jsonschema:"Set true only after deciding that no candidate is the intended third-level category."`
	CountTypeID               int64          `json:"count_type_id,omitempty" jsonschema:"Existing measurement-unit ID; required for quantity tracking."`
	CountTypeQuery            string         `json:"count_type_query,omitempty" jsonschema:"Measurement-unit name or abbreviation to resolve."`
	ModelNumber               string         `json:"model_number,omitempty"`
	ManufacturerPartNumber    string         `json:"manufacturer_part_number,omitempty"`
	EAN                       string         `json:"ean,omitempty"`
	GenericBarcode            string         `json:"generic_barcode,omitempty"`
	MaintenanceInterval       *int           `json:"maintenance_interval,omitempty" jsonschema:"Non-negative maintenance interval in days."`
	ItemCostPerDay            *float64       `json:"item_cost_per_day,omitempty" jsonschema:"Non-negative daily rental cost."`
	Weight                    *float64       `json:"weight,omitempty" jsonschema:"Non-negative weight in kilograms."`
	Height                    *float64       `json:"height,omitempty" jsonschema:"Non-negative height in centimeters."`
	Width                     *float64       `json:"width,omitempty" jsonschema:"Non-negative width in centimeters."`
	Depth                     *float64       `json:"depth,omitempty" jsonschema:"Non-negative depth in centimeters."`
	PowerConsumption          *float64       `json:"power_consumption,omitempty" jsonschema:"Non-negative power consumption in watts."`
	PositionInCategory        *int           `json:"position_in_category,omitempty" jsonschema:"Optional positive display position."`
	StockQuantity             *float64       `json:"stock_quantity,omitempty" jsonschema:"Non-negative initial quantity; requires an initial zone when greater than zero."`
	MinimumStockLevel         *float64       `json:"minimum_stock_level,omitempty" jsonschema:"Non-negative replenishment threshold."`
	PricePerUnit              *float64       `json:"price_per_unit,omitempty" jsonschema:"Non-negative unit price."`
	Attributes                map[string]any `json:"attributes,omitempty" jsonschema:"Additional structured technical attributes."`
	InitialDeviceQuantity     int            `json:"initial_device_quantity,omitempty" jsonschema:"Initial serialized devices, from 0 to 1000; only for individual tracking."`
	InitialZoneID             int64          `json:"initial_zone_id,omitempty" jsonschema:"Existing active storage-zone ID for initial stock or devices."`
	ProcurementProductID      int64          `json:"procurement_product_id,omitempty" jsonschema:"Optional active ProcurementCore product to link atomically."`
	AllowSimilarProduct       bool           `json:"allow_similar_product,omitempty" jsonschema:"Set true only after reviewing returned similar products and confirming this is distinct."`
	AcceptIncomplete          bool           `json:"accept_incomplete,omitempty" jsonschema:"Set true only after explicitly accepting the listed recommended data gaps."`
	ConfirmCreation           bool           `json:"confirm_creation,omitempty" jsonschema:"Set true only after showing the complete final draft and receiving explicit confirmation."`
}

type preparedWarehouseProduct struct {
	Draft              map[string]any
	MasterDataPlan     []map[string]any
	RequiredMissing    []string
	RecommendedMissing []string
	Questions          []map[string]any
	SimilarProducts    []map[string]any
	Ready              bool
}

func (p preparedWarehouseProduct) response(status string) map[string]any {
	return map[string]any{
		"creation_status": status, "ready_to_create": p.Ready, "draft": p.Draft, "master_data_plan": p.MasterDataPlan,
		"required_missing_fields": p.RequiredMissing, "recommended_missing_fields": p.RecommendedMissing,
		"questions_for_user": p.Questions, "similar_products": p.SimilarProducts,
		"transaction": "manufacturer, brand, category hierarchy, product, initial stock and initial devices are committed together or rolled back together",
	}
}

func registerWarehouseProductCreateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.products.prepare_create", "Prepare warehouse product creation", "Discover and validate all WarehouseCore product fields, fuzzy-resolve manufacturer, brand and category hierarchy, detect similar products, and return an atomic master-data/product draft with exact questions.", func(ctx context.Context, input WarehouseProductCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareWarehouseProductCreate(ctx, db, input)
		return prepared.response("draft"), warehouseProductDraftSources(prepared), nil, err
	})
	addCreateTool(server, "warehouse.products.create", "Create warehouse product", "Create one WarehouseCore product and explicitly approved missing master data in one transaction. First call warehouse.products.prepare_create, resolve every question, show the full draft and master-data plan, and obtain explicit confirmation.", func(ctx context.Context, input WarehouseProductCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareWarehouseProductCreate(ctx, db, input)
		if err != nil {
			return nil, nil, nil, err
		}
		if !prepared.Ready {
			return prepared.response("needs_input"), warehouseProductDraftSources(prepared), []string{"No data was changed. Ask the listed questions before retrying."}, nil
		}
		if len(prepared.RecommendedMissing) > 0 && !input.AcceptIncomplete {
			return prepared.response("recommended_input_required"), warehouseProductDraftSources(prepared), []string{"No data was changed. Fill the recommended fields or ask the user to explicitly accept the listed gaps."}, nil
		}
		if !input.ConfirmCreation {
			return prepared.response("confirmation_required"), warehouseProductDraftSources(prepared), []string{"No data was changed. Show this final draft and ask the user for explicit confirmation."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/products", http.MethodPost, prepared.Draft, &created); err != nil {
			return nil, nil, []string{"The WarehouseCore transaction was rejected or rolled back."}, err
		}
		return map[string]any{"creation_status": "created", "product": created, "master_data_plan": prepared.MasterDataPlan}, warehouseCreatedProductSources(created), nil, nil
	})
}

func prepareWarehouseProductCreate(ctx context.Context, db *store.Store, input WarehouseProductCreateInput) (preparedWarehouseProduct, error) {
	prepared := preparedWarehouseProduct{Draft: map[string]any{}, MasterDataPlan: []map[string]any{}, SimilarProducts: []map[string]any{}}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		prepared.require("name", "Wie soll das Warehouse-Produkt heißen?", nil)
	} else if len([]rune(name)) > 255 {
		prepared.require("name", "Der Produktname ist länger als 255 Zeichen. Wie lautet der kürzere Name?", nil)
	} else {
		prepared.Draft["name"] = name
	}

	productType := strings.ToLower(strings.TrimSpace(input.ProductType))
	if productType == "" {
		productType = "equipment"
	}
	if !containsString([]string{"equipment", "accessory", "consumable"}, productType) {
		prepared.require("product_type", "Welche Produktart ist gemeint: equipment, accessory oder consumable?", []string{"equipment", "accessory", "consumable"})
	} else {
		prepared.Draft["product_type"] = productType
	}
	trackingMode := strings.ToLower(strings.TrimSpace(input.TrackingMode))
	if trackingMode == "" {
		if productType == "equipment" {
			trackingMode = "individual"
		} else {
			trackingMode = "quantity"
		}
	}
	if !containsString([]string{"individual", "quantity", "none"}, trackingMode) || productType == "consumable" && trackingMode != "quantity" || productType == "equipment" && trackingMode == "quantity" {
		prepared.require("tracking_mode", "Welche zulässige Trackingart soll verwendet werden? Equipment: individual/none; Verbrauchsmaterial: quantity.", []string{"individual", "quantity", "none"})
	} else {
		prepared.Draft["tracking_mode"] = trackingMode
	}
	productKind := strings.ToLower(strings.TrimSpace(input.ProductKind))
	if productKind == "" {
		if productType == "consumable" {
			productKind = "consumable"
		} else {
			productKind = "standard"
		}
	}
	if !containsString([]string{"standard", "cable", "consumable", "container", "service"}, productKind) {
		prepared.require("product_kind", "Welche Produktklasse ist gemeint?", []string{"standard", "cable", "consumable", "container", "service"})
	} else {
		prepared.Draft["product_kind"] = productKind
	}

	if err := prepareWarehouseProductMasters(ctx, db, input, &prepared); err != nil {
		return prepared, err
	}
	if err := prepareWarehouseProductDetails(ctx, db, input, trackingMode, &prepared); err != nil {
		return prepared, err
	}
	if name != "" {
		similar, err := findSimilarWarehouseProducts(ctx, db, input)
		if err != nil {
			return prepared, err
		}
		prepared.SimilarProducts = similar
		exact := false
		for _, product := range similar {
			if product["match_kind"] == "exact" {
				exact = true
			}
		}
		if exact {
			prepared.require("duplicate_product", "Ein Produkt mit demselben Namen oder derselben eindeutigen Kennung existiert bereits. Verwende den bestehenden Datensatz.", similar)
		} else if len(similar) > 0 && !input.AllowSimilarProduct {
			prepared.require("similar_product_review", "Ähnliche Produkte wurden gefunden. Ist die Neuanlage wirklich ein eigener Artikel?", similar)
		}
	}

	prepared.RequiredMissing = unique(prepared.RequiredMissing)
	prepared.RecommendedMissing = unique(prepared.RecommendedMissing)
	prepared.Ready = len(prepared.RequiredMissing) == 0
	return prepared, nil
}

func (p *preparedWarehouseProduct) require(field, prompt string, options any) {
	if containsString(p.RequiredMissing, field) {
		return
	}
	p.RequiredMissing = append(p.RequiredMissing, field)
	p.Questions = append(p.Questions, question(field, prompt, "required", options))
}

func prepareWarehouseProductMasters(ctx context.Context, db *store.Store, input WarehouseProductCreateInput, prepared *preparedWarehouseProduct) error {
	manufacturer, options, err := selectWarehouseMaster(ctx, db, "manufacturer", strconv.FormatInt(input.ManufacturerID, 10), input.ManufacturerName)
	if err != nil {
		return err
	}
	if manufacturer != nil {
		prepared.Draft["manufacturer_id"] = manufacturer["id"]
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("manufacturer", "reference", manufacturer))
	} else if strings.TrimSpace(input.ManufacturerName) != "" && input.CreateManufacturer {
		prepared.Draft["manufacturer_name_input"] = strings.TrimSpace(input.ManufacturerName)
		if value := strings.TrimSpace(input.ManufacturerWebsite); value != "" {
			prepared.Draft["manufacturer_website_input"] = value
		}
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("manufacturer", "create", map[string]any{"name": strings.TrimSpace(input.ManufacturerName), "website": strings.TrimSpace(input.ManufacturerWebsite)}))
	} else {
		prepared.require("manufacturer", "Welcher Hersteller ist gemeint? Wähle einen Treffer oder bestätige die Neuanlage mit create_manufacturer=true.", options)
	}

	brand, brandOptions, err := selectWarehouseMaster(ctx, db, "brand", strconv.FormatInt(input.BrandID, 10), input.BrandName)
	if err != nil {
		return err
	}
	if brand != nil {
		prepared.Draft["brand_id"] = brand["id"]
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("brand", "reference", brand))
		if prepared.Draft["manufacturer_id"] == nil && brand["manufacturer_id"] != nil {
			prepared.Draft["manufacturer_id"] = brand["manufacturer_id"]
			prepared.removeRequirement("manufacturer")
			prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("manufacturer", "reference_via_brand", map[string]any{"id": brand["manufacturer_id"], "name": brand["manufacturer"]}))
		}
		if explicit := numericID(prepared.Draft["manufacturer_id"]); explicit > 0 && numericID(brand["manufacturer_id"]) > 0 && explicit != numericID(brand["manufacturer_id"]) {
			prepared.require("brand", "Die ausgewählte Marke gehört zu einem anderen Hersteller. Welche Kombination ist korrekt?", brandOptions)
		}
	} else if strings.TrimSpace(input.BrandName) != "" && input.CreateBrand {
		prepared.Draft["brand_name_input"] = strings.TrimSpace(input.BrandName)
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("brand", "create", map[string]any{"name": strings.TrimSpace(input.BrandName), "manufacturer": input.ManufacturerName}))
	} else if strings.TrimSpace(input.BrandName) != "" || input.BrandID > 0 {
		prepared.require("brand", "Welche Marke ist gemeint? Wähle einen Treffer oder bestätige die Neuanlage mit create_brand=true.", brandOptions)
	}

	category, categoryOptions, err := selectWarehouseMaster(ctx, db, "category", strconv.FormatInt(input.CategoryID, 10), input.CategoryName)
	if err != nil {
		return err
	}
	if category != nil {
		prepared.Draft["category_id"] = category["id"]
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("category", "reference", category))
	} else if strings.TrimSpace(input.CategoryName) != "" && input.CreateCategory {
		if strings.TrimSpace(input.CategoryAbbreviation) == "" {
			prepared.require("category_abbreviation", "Welche kurze Abkürzung soll die neue Kategorie erhalten?", nil)
		} else {
			prepared.Draft["category_name_input"] = strings.TrimSpace(input.CategoryName)
			prepared.Draft["category_abbreviation_input"] = strings.ToUpper(strings.TrimSpace(input.CategoryAbbreviation))
			prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan("category", "create", map[string]any{"name": strings.TrimSpace(input.CategoryName), "abbreviation": strings.ToUpper(strings.TrimSpace(input.CategoryAbbreviation))}))
		}
	} else {
		prepared.require("category", "Welche Kategorie ist gemeint? Wähle einen Treffer oder bestätige die Neuanlage mit create_category=true.", categoryOptions)
	}

	if err := prepareOptionalCategoryLevel(ctx, db, "subcategory", input.SubcategoryID, input.SubcategoryName, input.SubcategoryAbbreviation, input.CreateSubcategory, "subcategory_id", "subcategory_name_input", "subcategory_abbreviation_input", prepared); err != nil {
		return err
	}
	if err := prepareOptionalCategoryLevel(ctx, db, "third_category", input.ThirdCategoryID, input.ThirdCategoryName, input.ThirdCategoryAbbreviation, input.CreateThirdCategory, "subbiercategory_id", "third_category_name_input", "third_category_abbreviation_input", prepared); err != nil {
		return err
	}
	return nil
}

func prepareOptionalCategoryLevel(ctx context.Context, db *store.Store, entity, id, name, abbreviation string, create bool, idKey, nameKey, abbreviationKey string, prepared *preparedWarehouseProduct) error {
	selected, options, err := selectWarehouseMaster(ctx, db, entity, id, name)
	if err != nil {
		return err
	}
	if selected != nil {
		prepared.Draft[idKey] = selected["id"]
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan(entity, "reference", selected))
		if entity == "subcategory" && prepared.Draft["category_id"] != nil && fmt.Sprint(prepared.Draft["category_id"]) != fmt.Sprint(selected["category_id"]) {
			prepared.require(entity, "Die Unterkategorie gehört nicht zur ausgewählten Kategorie.", options)
		}
		if entity == "third_category" && prepared.Draft["subcategory_id"] != nil && fmt.Sprint(prepared.Draft["subcategory_id"]) != fmt.Sprint(selected["subcategory_id"]) {
			prepared.require(entity, "Die Kategorie der dritten Ebene gehört nicht zur ausgewählten Unterkategorie.", options)
		}
		return nil
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(id) == "" {
		return nil
	}
	if create {
		if strings.TrimSpace(name) == "" {
			prepared.require(entity, "Für die Neuanlage ist ein Name erforderlich.", options)
			return nil
		}
		prepared.Draft[nameKey] = strings.TrimSpace(name)
		if strings.TrimSpace(abbreviation) != "" {
			prepared.Draft[abbreviationKey] = strings.ToUpper(strings.TrimSpace(abbreviation))
		}
		prepared.MasterDataPlan = append(prepared.MasterDataPlan, masterPlan(entity, "create", map[string]any{"name": strings.TrimSpace(name), "abbreviation": strings.ToUpper(strings.TrimSpace(abbreviation))}))
		return nil
	}
	prepared.require(entity, "Wähle einen passenden Treffer oder bestätige die Neuanlage mit dem zugehörigen create_*-Feld.", options)
	return nil
}

func (p *preparedWarehouseProduct) removeRequirement(field string) {
	missing := p.RequiredMissing[:0]
	for _, value := range p.RequiredMissing {
		if value != field {
			missing = append(missing, value)
		}
	}
	p.RequiredMissing = missing
	questions := p.Questions[:0]
	for _, value := range p.Questions {
		if fmt.Sprint(value["field"]) != field {
			questions = append(questions, value)
		}
	}
	p.Questions = questions
}

func prepareWarehouseProductDetails(ctx context.Context, db *store.Store, input WarehouseProductCreateInput, trackingMode string, prepared *preparedWarehouseProduct) error {
	if description := strings.TrimSpace(input.Description); description != "" {
		prepared.Draft["description"] = description
	} else {
		prepared.RecommendedMissing = append(prepared.RecommendedMissing, "description")
		prepared.Questions = append(prepared.Questions, question("description", "Welche Funktion und welchen Einsatzzweck hat das Produkt?", "recommended", nil))
	}
	for key, value := range map[string]string{"model_number": input.ModelNumber, "manufacturer_part_number": input.ManufacturerPartNumber, "ean": input.EAN, "generic_barcode": input.GenericBarcode} {
		if strings.TrimSpace(value) != "" {
			prepared.Draft[key] = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(input.ModelNumber) == "" {
		prepared.RecommendedMissing = append(prepared.RecommendedMissing, "model_number")
		prepared.Questions = append(prepared.Questions, question("model_number", "Welche Modellbezeichnung führt der Hersteller?", "recommended", nil))
	}
	if trackingMode == "quantity" {
		countType, options, err := selectWarehouseMaster(ctx, db, "count_type", strconv.FormatInt(input.CountTypeID, 10), input.CountTypeQuery)
		if err != nil {
			return err
		}
		if countType == nil {
			prepared.require("count_type_id", "Welche vorhandene Mengeneinheit soll verwendet werden?", options)
		} else {
			prepared.Draft["count_type_id"] = countType["id"]
		}
	}
	if input.InitialZoneID > 0 {
		zone, _, err := selectWarehouseMaster(ctx, db, "zone", strconv.FormatInt(input.InitialZoneID, 10), "")
		if err != nil {
			return err
		}
		if zone == nil {
			prepared.require("initial_zone_id", "Der angegebene aktive Lagerplatz wurde nicht gefunden.", nil)
		} else {
			prepared.Draft["initial_zone_id"] = zone["id"]
		}
	}
	if input.InitialDeviceQuantity < 0 || input.InitialDeviceQuantity > 1000 || input.InitialDeviceQuantity > 0 && trackingMode != "individual" {
		prepared.require("initial_device_quantity", "Die Anfangsmenge muss zwischen 0 und 1000 liegen und benötigt Trackingart individual.", nil)
	} else if input.InitialDeviceQuantity > 0 {
		prepared.Draft["initial_device_quantity"] = input.InitialDeviceQuantity
	}
	if input.StockQuantity != nil && *input.StockQuantity > 0 && input.InitialZoneID == 0 {
		prepared.require("initial_zone_id", "Für einen positiven Anfangsbestand ist ein aktiver Lagerplatz erforderlich.", nil)
	}
	for _, number := range []struct {
		key   string
		value *float64
	}{
		{"item_cost_per_day", input.ItemCostPerDay}, {"weight", input.Weight}, {"height", input.Height}, {"width", input.Width},
		{"depth", input.Depth}, {"power_consumption", input.PowerConsumption}, {"stock_quantity", input.StockQuantity},
		{"min_stock_level", input.MinimumStockLevel}, {"price_per_unit", input.PricePerUnit},
	} {
		if number.value == nil {
			continue
		}
		if *number.value < 0 {
			prepared.require(number.key, "Der Wert darf nicht negativ sein.", nil)
		} else {
			prepared.Draft[number.key] = *number.value
		}
	}
	if input.MaintenanceInterval != nil {
		if *input.MaintenanceInterval < 0 {
			prepared.require("maintenance_interval", "Das Wartungsintervall darf nicht negativ sein.", nil)
		} else {
			prepared.Draft["maintenance_interval"] = *input.MaintenanceInterval
		}
	}
	if input.PositionInCategory != nil {
		if *input.PositionInCategory < 1 {
			prepared.require("position_in_category", "Die Kategorieposition muss mindestens 1 sein.", nil)
		} else {
			prepared.Draft["pos_in_category"] = *input.PositionInCategory
		}
	}
	if input.Attributes != nil {
		prepared.Draft["attributes"] = input.Attributes
	}
	if input.ProcurementProductID > 0 {
		rows, err := db.Query(ctx, `SELECT id,name,sku FROM proc_products WHERE id=$1 AND active=true`, input.ProcurementProductID)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			prepared.require("procurement_product_id", "Das angegebene aktive ProcurementCore-Produkt wurde nicht gefunden.", nil)
		} else {
			prepared.Draft["procurement_product_id"] = input.ProcurementProductID
		}
	}
	return nil
}

func selectWarehouseMaster(ctx context.Context, db *store.Store, entity, rawID, name string) (map[string]any, []map[string]any, error) {
	rawID = strings.TrimSpace(rawID)
	if rawID == "0" {
		rawID = ""
	}
	if rawID != "" {
		row, err := warehouseMasterByID(ctx, db, entity, rawID)
		if err != nil {
			return nil, nil, err
		}
		if row != nil {
			return row, []map[string]any{row}, nil
		}
		return nil, nil, nil
	}
	if strings.TrimSpace(name) == "" {
		return nil, nil, nil
	}
	candidates, err := warehouseMasterCandidates(ctx, db, entity, name, 10)
	if err != nil {
		return nil, nil, err
	}
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" {
			return candidate, candidates, nil
		}
	}
	return nil, candidates, nil
}

func warehouseMasterByID(ctx context.Context, db *store.Store, entity, id string) (map[string]any, error) {
	var statement string
	switch entity {
	case "manufacturer":
		statement = `SELECT manufacturerid AS id,name,COALESCE(website,'') AS context FROM manufacturer WHERE manufacturerid::text=$1`
	case "brand":
		statement = `SELECT b.brandid AS id,b.name,COALESCE(m.name,'') AS context,b.manufacturerid AS manufacturer_id,m.name AS manufacturer FROM brands b LEFT JOIN manufacturer m ON m.manufacturerid=b.manufacturerid WHERE b.brandid::text=$1`
	case "category":
		statement = `SELECT categoryid AS id,name,abbreviation AS context,abbreviation FROM categories WHERE categoryid::text=$1`
	case "subcategory":
		statement = `SELECT s.subcategoryid AS id,s.name,concat_ws(' · ',c.name,s.abbreviation) AS context,s.categoryid AS category_id,c.name AS category FROM subcategories s JOIN categories c ON c.categoryid=s.categoryid WHERE s.subcategoryid::text=$1`
	case "third_category":
		statement = `SELECT t.subbiercategoryid AS id,t.name,concat_ws(' · ',c.name,s.name,t.abbreviation) AS context,t.subcategoryid AS subcategory_id,s.name AS subcategory,s.categoryid AS category_id,c.name AS category FROM subbiercategories t JOIN subcategories s ON s.subcategoryid=t.subcategoryid JOIN categories c ON c.categoryid=s.categoryid WHERE t.subbiercategoryid::text=$1`
	case "count_type":
		statement = `SELECT count_type_id AS id,name,abbreviation AS context,abbreviation FROM count_types WHERE count_type_id::text=$1`
	case "zone":
		statement = `SELECT zone_id AS id,name,concat_ws(' · ',code,location,process_role) AS context,code FROM storage_zones WHERE is_active=true AND zone_id::text=$1`
	default:
		return nil, fmt.Errorf("unsupported warehouse master entity %q", entity)
	}
	rows, err := db.Query(ctx, statement, id)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func findSimilarWarehouseProducts(ctx context.Context, db *store.Store, input WarehouseProductCreateInput) ([]map[string]any, error) {
	rows, err := db.Query(ctx, `SELECT p.productid AS product_id,p.name,p.product_code,p.model_number,p.manufacturer_part_number,p.ean,p.generic_barcode,m.name AS manufacturer FROM products p LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid WHERE COALESCE(p.lifecycle_status,'active')='active' ORDER BY p.updated_at DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, row := range rows {
		score := warehouseMatchScore(input.Name, fmt.Sprint(row["name"]), strings.Join([]string{fmt.Sprint(row["manufacturer"]), fmt.Sprint(row["model_number"])}, " "))
		exactIdentifier := sameNonBlank(input.EAN, row["ean"]) || sameNonBlank(input.GenericBarcode, row["generic_barcode"]) || sameNonBlank(input.ManufacturerPartNumber, row["manufacturer_part_number"])
		exactName := normalizeIdentity(input.Name) == normalizeIdentity(fmt.Sprint(row["name"]))
		if score < 55 && !exactIdentifier {
			continue
		}
		candidate := cloneMap(row)
		candidate["match_score"] = score
		candidate["match_kind"] = "similar"
		if exactName || exactIdentifier {
			candidate["match_kind"] = "exact"
		}
		result = append(result, candidate)
		if len(result) >= 20 {
			break
		}
	}
	return result, nil
}

func sameNonBlank(value string, candidate any) bool {
	return strings.TrimSpace(value) != "" && strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(fmt.Sprint(candidate)))
}

func masterPlan(entity, action string, value map[string]any) map[string]any {
	return map[string]any{"entity": entity, "action": action, "value": value}
}

func warehouseProductDraftSources(prepared preparedWarehouseProduct) []Source {
	sources := []Source{{Service: "warehousecore", Entity: "product_draft"}}
	for _, product := range prepared.SimilarProducts {
		sources = append(sources, Source{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(product["product_id"])})
	}
	return sources
}

func warehouseCreatedProductSources(created map[string]any) []Source {
	sources := []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(created["product_id"])}}
	for key, entity := range map[string]string{"manufacturer_id": "manufacturer", "brand_id": "brand", "category_id": "category", "subcategory_id": "subcategory", "subbiercategory_id": "third_category"} {
		if created[key] != nil && fmt.Sprint(created[key]) != "" && fmt.Sprint(created[key]) != "<nil>" {
			sources = append(sources, Source{Service: "warehousecore", Entity: entity, ID: fmt.Sprint(created[key])})
		}
	}
	return sources
}

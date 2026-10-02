package mcpserver

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EntitySchemaInput struct {
	Entity string `json:"entity,omitempty" jsonschema:"Exact writable entity name. Leave empty to list every supported schema."`
}

type writableEntitySchema struct {
	Entity         string
	Service        string
	Input          any
	UpdateInput    any
	ItemInput      any
	DeleteInput    any
	LifecycleInput any
	RevertInput    any
	Required       []string
	Operations     []string
	Notes          []string
}

func registerSchemaTools(server *mcp.Server) {
	addTool(server, "cores.entities.schema", "Describe writable entity schemas", "Return writable fields, types, validation descriptions, required fields and supported guided operations for Cores entities. Call this before preparing a create or update when field availability is unclear.", func(_ context.Context, input EntitySchemaInput) (any, []Source, []string, error) {
		catalog := writableEntitySchemas()
		entity := strings.TrimSpace(input.Entity)
		if entity != "" {
			schema, ok := catalog[entity]
			if !ok {
				return nil, nil, nil, fmt.Errorf("unsupported entity %q; supported entities: %s", entity, strings.Join(sortedMapKeys(catalog), ", "))
			}
			return renderWritableEntitySchema(schema), []Source{{Service: "cores-mcp", Entity: "entity_schema", ID: entity}}, nil, nil
		}
		result := make([]map[string]any, 0, len(catalog))
		for _, name := range sortedMapKeys(catalog) {
			result = append(result, renderWritableEntitySchema(catalog[name]))
		}
		return result, []Source{{Service: "cores-mcp", Entity: "entity_schema"}}, nil, nil
	})
}

func writableEntitySchemas() map[string]writableEntitySchema {
	return map[string]writableEntitySchema{
		"rental.customers":             {Entity: "rental.customers", Service: "rentalcore", Input: RentalCustomerInput{}, UpdateInput: RentalCustomerInput{}, LifecycleInput: RentalMasterControl{}, Required: []string{"company_name|name|first_name|last_name", "is_customer|is_supplier"}, Operations: []string{"resolve", "get", "search", "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Current administrator/action rights, complete privileged field preview, exact microsecond record/context versions, explicit duplicate resolution, elevated record/draft-bound confirmation and atomic audit/replay. Lifecycle preserves every business field; active jobs block archive. Default reads and histories exclude contact details, addresses, notes and external sync IDs. Empty optional strings clear; omitted fields preserve."}},
		"rental.venues":                {Entity: "rental.venues", Service: "rentalcore", Input: RentalVenueInput{}, UpdateInput: RentalVenueInput{}, LifecycleInput: RentalMasterControl{}, Required: []string{"name"}, Operations: []string{"resolve", "get", "search", "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Current administrator/action rights, complete privileged field preview, exact microsecond record/context versions, explicit duplicate resolution, elevated record/draft-bound confirmation and atomic audit/replay. Lifecycle preserves every business field; active jobs block archive. Normal selectors exclude archives; default reads/history omit contacts, addresses and notes. Empty optional strings clear; omitted fields preserve."}},
		"warehouse.maintenance_plans":  {Entity: "warehouse.maintenance_plans", Service: "warehousecore", Input: WarehouseMaintenancePlanInput{}, UpdateInput: WarehouseMaintenancePlanInput{}, LifecycleInput: WarehouseMaintenancePlanInput{}, Required: []string{"device_id", "name", "interval_days", "next_due_at"}, Operations: []string{"search", "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Matching admin/action scope, complete preview, exact device and existing plan versions, explicit confirmation and durable replay. Lifecycle preserves identity/history and blocks open work. Due planned orders, device next date and audits are one transaction."}},
		"warehouse.maintenance_orders": {Entity: "warehouse.maintenance_orders", Service: "warehousecore", Input: WarehouseMaintenanceOrderInput{}, UpdateInput: WarehouseMaintenanceOrderInput{}, LifecycleInput: WarehouseMaintenanceOrderInput{}, Required: []string{"device_id", "title"}, Operations: []string{"search", "prepare_create", "create", "prepare_update", "update", "prepare_transition", "transition", "prepare_complete", "complete", "prepare_cancel", "cancel", "prepare_reopen", "reopen", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Matching admin/action scopes, complete draft/diff, exact order/device/linked plan versions, elevated terminal/lifecycle confirmation and durable atomic order/event/device/plan/audit replay. Costs require explicit financial scope; only terminal orders can be archived. Defect IDs refer to canonical orders and legacy IDs are separate."}},
		"warehouse.defects":            {Entity: "warehouse.defects", Service: "warehousecore", Input: WarehouseMaintenanceOrderInput{}, UpdateInput: WarehouseMaintenanceOrderInput{}, LifecycleInput: WarehouseMaintenanceOrderInput{}, Required: []string{"device_id", "title"}, Operations: []string{"search", "prepare_create", "create", "prepare_update", "update", "prepare_transition", "transition", "prepare_complete", "complete", "prepare_cancel", "cancel", "prepare_reopen", "reopen", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Matching admin/action scopes, complete draft/diff, exact order/device/linked plan versions, elevated terminal/lifecycle confirmation and durable atomic order/event/device/plan/audit replay. Costs require explicit financial scope; only terminal orders can be archived. Defect IDs refer to canonical orders and legacy IDs are separate."}},
		"warehouse.cases":              {Entity: "warehouse.cases", Service: "warehousecore", Input: WarehouseCaseInput{}, UpdateInput: WarehouseCaseInput{}, LifecycleInput: WarehouseCaseInput{}, Required: []string{"name"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Exact case version covers metadata, contents, templates and nesting. Administrator/create-update-archive scope, explicit confirmation, durable idempotency and atomic audit required. Lifecycle blocks contents, jobs, tasks, nesting and active workflow; barcode identities remain reserved while archived."}},
		"rental.jobs":                  {Entity: "rental.jobs", Service: "rentalcore", Input: JobCreateInput{}, UpdateInput: JobUpdateInput{}, Required: []string{"description", "customer_id|customer_query", "start_date", "end_date"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}},
		"rental.requirements":          {Entity: "rental.requirements", Service: "rentalcore", Input: RequirementCreateInput{}, UpdateInput: RequirementUpdateInput{}, Required: []string{"job_id|job_query", "product_id|product_query", "quantity"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}},
		"rental.device_assignments":    {Entity: "rental.device_assignments", Service: "rentalcore", Input: JobDeviceAssignInput{}, Required: []string{"job_id|job_query", "device_id"}, Operations: []string{"prepare_assign", "assign"}},
		"warehouse.device_batches":     {Entity: "warehouse.device_batches", Service: "warehousecore", Input: WarehouseDeviceBulkCreateInput{}, Required: []string{"devices"}, Operations: []string{"prepare_bulk_create", "bulk_create"}, Notes: []string{"1-100 complete device drafts; admin/create scope, combined capacity and reserved identity validation. Devices, scan identities, per-device audit and durable replay are one transaction."}},
		"warehouse.devices":            {Entity: "warehouse.devices", Service: "warehousecore", Input: WarehouseDeviceCreateInput{}, UpdateInput: WarehouseDeviceUpdateInput{}, LifecycleInput: WarehouseDeviceLifecycleInput{}, RevertInput: WarehouseDeviceRevertInput{}, Required: []string{"product_id"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "prepare_revert_update", "revert_update", "audit_history", "prepare_update_status", "update_status"}, Notes: []string{"Admin and matching create/update/archive scope required. Bulk creation uses warehouse.device_batches; scan codes and serial numbers remain reserved while archived. Exact full-device version covers every writer. Lifecycle changes require dependency review and a device-bound phrase. Revert is limited to the current user latest unchanged MCP device.update audit; an inverse diff and audit-bound confirmation are required. Physical movements, condition and maintenance workflows are separate. Audit history excludes notes, IP and raw JSON."}},
		"warehouse.packages":           {Entity: "warehouse.packages", Service: "warehousecore", Input: WarehousePackageCreateInput{}, UpdateInput: WarehousePackageUpdateInput{}, LifecycleInput: WarehousePackageLifecycleInput{}, ItemInput: WarehousePackageItem{}, Required: []string{"name", "items"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Admin and matching create/update/archive scope required. Lifecycle requires full version, job/reservation review and package-bound confirmation. Restore validates all products. Both lifecycle actions disable website visibility; history and item row IDs remain. Code and ID are immutable. Package and all product lines are committed atomically with audit and idempotency. Exact version covers metadata and contents. Prices and contents used in any job are protected; create a new package instead. No stock movement, mirror product or file upload."}},
		"warehouse.product_relations":  {Entity: "warehouse.product_relations", Service: "warehousecore", Input: WarehouseRelationInput{}, UpdateInput: WarehouseRelationInput{}, LifecycleInput: WarehouseRelationInput{}, Required: []string{"product_id", "dependency_product_id"}, Operations: []string{"search", "get", "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Typed fields, immutable product endpoints, exact record/product/graph context and active jobs are validated at the owner. Lifecycle retains identity/history; active business selectors exclude archives. Real admin/action scope, elevated confirmation, atomic audit/product versions and durable replay are mandatory."}},
		"warehouse.products":           {Entity: "warehouse.products", Service: "warehousecore", Input: WarehouseProductCreateInput{}, UpdateInput: WarehouseProductUpdateInput{}, LifecycleInput: WarehouseProductLifecycleInput{}, Required: []string{"name", "category_id|category_name", "manufacturer_id|manufacturer_name"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "prepare_link_relation", "link_relation"}, Notes: []string{"Missing master data is created only when its create_* flag is explicitly true.", "Manufacturer, brand, category hierarchy, product, initial stock and initial devices are committed atomically.", "prepare_update and update accept WarehouseProductUpdateInput; call prepare_update to see its full diff and exact expected_updated_at.", "Lifecycle operations accept WarehouseProductLifecycleInput and require a dedicated archive scope, version, dependency preview and record-bound confirmation.", "Product relationships use owner previews with complete product/relation/graph context, exact versions and atomic audit/replay; see warehouse.product_relations for full lifecycle."}},
		"warehouse.manufacturers":      {Entity: "warehouse.manufacturers", Service: "warehousecore", Input: WarehouseManufacturerCreateInput{}, UpdateInput: WarehouseManufacturerUpdateInput{}, LifecycleInput: WarehouseMasterLifecycleInput{}, Required: []string{"name"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "resolve_or_create_via_product", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Standalone creation returns manufacturer_id for warehouse.brands.create and warehouse.products.update. Updates support name and website (empty clears), full diff, exact version, administrator permission, confirmation and idempotency."}},
		"warehouse.brands":             {Entity: "warehouse.brands", Service: "warehousecore", Input: WarehouseBrandCreateInput{}, UpdateInput: WarehouseBrandUpdateInput{}, LifecycleInput: WarehouseMasterLifecycleInput{}, Required: []string{"name", "manufacturer_id"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "resolve_or_create_via_product", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Standalone creation requires an existing manufacturer and returns brand_id for warehouse.products.update. Updates support name and manufacturer reassociation; clear_manufacturer explicitly removes the association. Linked products must already use the proposed manufacturer. Exact version, administrator permission, full diff, confirmation and idempotency are required."}},
		"warehouse.categories":         {Entity: "warehouse.categories", Service: "warehousecore", Input: WarehouseCategoryCreateInput{}, UpdateInput: WarehouseCategoryUpdateInput{}, DeleteInput: WarehouseCategoryDeleteInput{}, LifecycleInput: WarehouseCategoryLifecycleInput{}, Required: []string{"name", "abbreviation"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "prepare_delete", "delete", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history", "resolve_or_create_via_product"}, Notes: []string{"Soft archive retains IDs, all metadata, hierarchy and historical products. Active descendants block archive; restoration validates parent ancestry and identity. Archive scope, exact version/dependency fingerprint, elevated confirmation and atomic audit/replay are required.", "Permanent removal is limited to unused categories without children; dedicated delete scope, unchanged version, explicit confirmation and exact record-bound phrase are required. No cascade; audit history remains.", "Updates preserve IDs, preview the complete diff and linked record counts, and require admin/update scope, exact version, confirmation and idempotency. Conflicting product assignments block parent changes."}},
		"warehouse.subcategories":      {Entity: "warehouse.subcategories", Service: "warehousecore", Input: WarehouseSubcategoryCreateInput{}, UpdateInput: WarehouseSubcategoryUpdateInput{}, DeleteInput: WarehouseSubcategoryDeleteInput{}, LifecycleInput: WarehouseCategoryLifecycleInput{}, Required: []string{"name", "category_id"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "prepare_delete", "delete", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history", "resolve_or_create_via_product"}, Notes: []string{"Soft archive retains IDs, all metadata, hierarchy and historical products. Active descendants block archive; restoration validates parent ancestry and identity. Archive scope, exact version/dependency fingerprint, elevated confirmation and atomic audit/replay are required.", "Permanent removal is limited to unused categories without children; dedicated delete scope, unchanged version, explicit confirmation and exact record-bound phrase are required. No cascade; audit history remains.", "Updates preserve IDs, preview the complete diff and linked record counts, and require admin/update scope, exact version, confirmation and idempotency. Conflicting product assignments block parent changes."}},
		"warehouse.third_categories":   {Entity: "warehouse.third_categories", Service: "warehousecore", Input: WarehouseThirdCategoryCreateInput{}, UpdateInput: WarehouseThirdCategoryUpdateInput{}, DeleteInput: WarehouseThirdCategoryDeleteInput{}, LifecycleInput: WarehouseCategoryLifecycleInput{}, Required: []string{"name", "subcategory_id"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "prepare_delete", "delete", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history", "resolve_or_create_via_product"}, Notes: []string{"Soft archive retains IDs, all metadata, hierarchy and historical products. Active descendants block archive; restoration validates parent ancestry and identity. Archive scope, exact version/dependency fingerprint, elevated confirmation and atomic audit/replay are required.", "Permanent removal is limited to unused categories without children; dedicated delete scope, unchanged version, explicit confirmation and exact record-bound phrase are required. No cascade; audit history remains.", "Updates preserve IDs, preview the complete diff and linked record counts, and require admin/update scope, exact version, confirmation and idempotency. Conflicting product assignments block parent changes."}},
		"warehouse.locations":          {Entity: "warehouse.locations", Service: "warehousecore", Input: WarehouseLocationCreateInput{}, UpdateInput: WarehouseLocationUpdateInput{}, LifecycleInput: WarehouseLocationLifecycleInput{}, Required: []string{"code", "name"}, Operations: []string{"resolve", "prepare_create", "create", "prepare_update", "update", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Lifecycle requires admin/archive scope, exact version, location-bound phrase and dependency review. Inventory, active descendants, home cases and open tasks/counts block both actions. Restore validates hierarchy and identity; unchanged audited archives preserve available/blocked/maintenance, otherwise restore blocked. History and all metadata remain; no cascade or stock movement.", "Updates require an active location, full diff, exact expected_updated_at, explicit confirmation and idempotency. Nullable update fields use clear_fields. Hierarchy cycles and limits below occupancy are blocked; operational status is preserved.", "The parent location must be active. An omitted barcode becomes LOC- plus the code. Creation starts in the available state."}},
		"warehouse.inventory_counts":   {Entity: "warehouse.inventory_counts", Service: "warehousecore", Input: WarehouseInventoryInput{}, UpdateInput: WarehouseInventoryInput{}, LifecycleInput: WarehouseInventoryInput{}, Required: []string{"zone_id"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_set_lines", "set_lines", "prepare_review", "review", "prepare_return_for_counting", "return_for_counting", "prepare_approve", "approve", "prepare_cancel", "cancel", "prepare_archive", "archive", "prepare_restore", "restore", "search", "get", "audit_history"}, Notes: []string{"lines entries: exact item_type (device/product/case), item_key (canonical ID), and exactly one counted_quantity (replacement, zero allowed) or clear_counted (existing line only).", "Exact count/context versions; line/event writers advance count version. Blind expected quantities stay hidden until confirmed review. Review requires explicit acknowledgement to zero missing lines. Only the separate approve scope books stock; legacy write does not grant inventory approval. Count, physical adjustments, movements, events, per-entity audits and durable replay commit together. Terminal restore retains status/history; recount creates a new count."}},
		"warehouse.tasks":              {Entity: "warehouse.tasks", Service: "warehousecore", Input: WarehouseTaskCreateInput{}, UpdateInput: WarehouseTaskInput{}, LifecycleInput: WarehouseTaskInput{}, Required: []string{"task_type", "context"}, Operations: []string{"search", "prepare_create", "create", "prepare_update", "update", "prepare_start", "start", "prepare_complete", "complete", "prepare_cancel", "cancel", "prepare_reopen", "reopen", "prepare_archive", "archive", "prepare_restore", "restore", "audit_history"}, Notes: []string{"Admin/action scope, exact task/reference versions, preview/diff, explicit confirmation and atomic audit/events/durable replay. Completing a task acknowledges work; inventory movements are separate explicit workflows. Only terminal tasks can be archived; restore preserves terminal status/history and reopening is separate."}},
		"warehouse.movements":          {Entity: "warehouse.movements", Service: "warehousecore", Input: WarehouseMovementCreateInput{}, Required: []string{"scan_code", "action"}, Operations: []string{"prepare_create", "create"}},
		"warehouse.device_status":      {Entity: "warehouse.device_status", Service: "warehousecore", Input: DeviceStatusUpdateInput{}, Required: []string{"device_id", "status|condition_status"}, Operations: []string{"prepare_update", "update"}},
		"procurement.products":         {Entity: "procurement.products", Service: "procurementcore", Input: ProductCreateInput{}, UpdateInput: ProductUpdateInput{}, Required: []string{"sku", "name", "category_id"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}, Notes: []string{"Update supports active=false to archive and active=true to restore. Confirmed updates require the exact expected_updated_at from prepare_update."}},
		"procurement.product_links":    {Entity: "procurement.product_links", Service: "cores", Input: ProductLinkInput{}, Required: []string{"procurement_product_id", "warehouse_product_id"}, Operations: []string{"prepare_link", "link"}, Notes: []string{"One-to-one link between active ProcurementCore and WarehouseCore products. Replacing a link is blocked by receipts or open purchase orders."}},
		"procurement.offers":           {Entity: "procurement.offers", Service: "procurementcore", Input: OfferCreateInput{}, UpdateInput: OfferUpdateInput{}, Required: []string{"product_id", "supplier_id", "price_cents"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}, Notes: []string{"Updates require an exact version and full diff. active=false archives an offer and active=true restores it."}},
		"procurement.suppliers":        {Entity: "procurement.suppliers", Service: "procurementcore", Input: SupplierCreateInput{}, UpdateInput: SupplierUpdateInput{}, Required: []string{"name", "code"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}, Notes: []string{"Update fields are optional and shown as a complete before/after diff; confirmed updates require the exact expected_updated_at from prepare_update.", "Set active=false to deactivate without deleting supplier history."}},
		"procurement.categories":       {Entity: "procurement.categories", Service: "procurementcore", Input: CategoryCreateInput{}, UpdateInput: CategoryUpdateInput{}, Required: []string{"name"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}, Notes: []string{"Parameter definitions contain a key, label, type, optional unit, and select options.", "Updates replace the entire parameter schema and require a full diff, exact expected_updated_at, and confirmation."}},
		"procurement.requisitions":     {Entity: "procurement.requisitions", Service: "procurementcore", Input: RequisitionDraftInput{}, UpdateInput: RequisitionDraftInput{}, Required: []string{"title", "lines"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_submit", "submit", "prepare_decide", "decide"}, Notes: []string{"Only the requester or a procurement administrator may edit or submit a draft. Decisions require a different administrator."}},
		"procurement.orders":           {Entity: "procurement.orders", Service: "procurementcore", Input: PurchaseOrderCreateInput{}, UpdateInput: OrderDraftUpdateInput{}, Required: []string{"supplier_id|supplier_query", "lines"}, Operations: []string{"prepare_create", "create", "prepare_update", "update", "prepare_transition", "transition", "prepare_receive", "receive"}, Notes: []string{"MCP creates draft orders only. prepare_update covers all draft fields and lines. Status transitions use the separate procurement approval scope and record-bound confirmation."}},
		"planner.plans":                {Entity: "planner.plans", Service: "plannercore", Input: PlannerPlanCreateInput{}, Required: []string{"name"}, Operations: []string{"prepare_create", "create"}},
		"planner.tasks":                {Entity: "planner.tasks", Service: "plannercore", Input: PlannerTaskCreateInput{}, Required: []string{"plan_id", "title"}, Operations: []string{"prepare_create", "create"}},
	}
}

func renderWritableEntitySchema(schema writableEntitySchema) map[string]any {
	result := map[string]any{"entity": schema.Entity, "service": schema.Service, "operations": schema.Operations, "required": schema.Required, "fields": renderWritableFields(schema.Input, schema.Required), "notes": schema.Notes}
	if schema.UpdateInput != nil {
		result["update_fields"] = renderWritableFields(schema.UpdateInput, nil)
	}
	if schema.ItemInput != nil {
		result["item_fields"] = renderWritableFields(schema.ItemInput, []string{"product_id", "quantity"})
	}
	if schema.DeleteInput != nil {
		result["delete_fields"] = renderWritableFields(schema.DeleteInput, nil)
	}
	if schema.LifecycleInput != nil {
		result["lifecycle_fields"] = renderWritableFields(schema.LifecycleInput, nil)
	}
	if schema.RevertInput != nil {
		result["revert_fields"] = renderWritableFields(schema.RevertInput, nil)
	}
	return result
}

func renderWritableFields(input any, requiredFields []string) []map[string]any {
	required := map[string]bool{}
	requiredGroups := map[string]string{}
	for _, field := range requiredFields {
		if strings.Contains(field, "|") {
			for _, alternative := range strings.Split(field, "|") {
				requiredGroups[alternative] = field
			}
		} else {
			required[field] = true
		}
	}
	typeOf := reflect.TypeOf(input)
	fields := make([]map[string]any, 0, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.Anonymous {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				fields = append(fields, renderWritableFields(reflect.New(embedded).Elem().Interface(), requiredFields)...)
			}
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		entry := map[string]any{"name": name, "type": jsonTypeName(field.Type), "required": required[name]}
		if group := requiredGroups[name]; group != "" {
			entry["required_one_of"] = strings.Split(group, "|")
		}
		if description := field.Tag.Get("jsonschema"); description != "" {
			entry["validation"] = description
		}
		nested := field.Type
		for nested.Kind() == reflect.Pointer {
			nested = nested.Elem()
		}
		if nested.Kind() == reflect.Struct {
			entry["fields"] = renderWritableFields(reflect.New(nested).Elem().Interface(), nil)
		}
		if nested.Kind() == reflect.Slice || nested.Kind() == reflect.Array {
			item := nested.Elem()
			for item.Kind() == reflect.Pointer {
				item = item.Elem()
			}
			if item.Kind() == reflect.Struct {
				entry["item_fields"] = renderWritableFields(reflect.New(item).Elem().Interface(), nil)
			}
		}
		fields = append(fields, entry)
	}
	return fields
}

func jsonTypeName(value reflect.Type) string {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array<" + jsonTypeName(value.Elem()) + ">"
	default:
		return "string"
	}
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

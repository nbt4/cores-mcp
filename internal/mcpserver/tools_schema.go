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
	Entity     string
	Service    string
	Input      any
	Required   []string
	Operations []string
	Notes      []string
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
		"rental.jobs":               {Entity: "rental.jobs", Service: "rentalcore", Input: JobCreateInput{}, Required: []string{"description", "customer_id|customer_query", "start_date", "end_date"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}},
		"rental.requirements":       {Entity: "rental.requirements", Service: "rentalcore", Input: RequirementCreateInput{}, Required: []string{"job_id|job_query", "product_id|product_query", "quantity"}, Operations: []string{"prepare_create", "create", "prepare_update", "update"}},
		"rental.device_assignments": {Entity: "rental.device_assignments", Service: "rentalcore", Input: JobDeviceAssignInput{}, Required: []string{"job_id|job_query", "device_id"}, Operations: []string{"prepare_assign", "assign"}},
		"warehouse.products":        {Entity: "warehouse.products", Service: "warehousecore", Input: WarehouseProductCreateInput{}, Required: []string{"name", "category_id|category_name", "manufacturer_id|manufacturer_name"}, Operations: []string{"prepare_create", "create"}, Notes: []string{"Missing master data is created only when its create_* flag is explicitly true.", "Manufacturer, brand, category hierarchy, product, initial stock and initial devices are committed atomically."}},
		"warehouse.manufacturers":   {Entity: "warehouse.manufacturers", Service: "warehousecore", Input: WarehouseMasterResolveInput{}, Required: []string{"query"}, Operations: []string{"resolve", "resolve_or_create_via_product"}},
		"warehouse.brands":          {Entity: "warehouse.brands", Service: "warehousecore", Input: WarehouseMasterResolveInput{}, Required: []string{"query"}, Operations: []string{"resolve", "resolve_or_create_via_product"}},
		"warehouse.categories":      {Entity: "warehouse.categories", Service: "warehousecore", Input: WarehouseMasterResolveInput{}, Required: []string{"query"}, Operations: []string{"resolve", "resolve_or_create_via_product"}},
		"warehouse.tasks":           {Entity: "warehouse.tasks", Service: "warehousecore", Input: WarehouseTaskCreateInput{}, Required: []string{"task_type", "context"}, Operations: []string{"prepare_create", "create"}},
		"warehouse.movements":       {Entity: "warehouse.movements", Service: "warehousecore", Input: WarehouseMovementCreateInput{}, Required: []string{"scan_code", "action"}, Operations: []string{"prepare_create", "create"}},
		"warehouse.device_status":   {Entity: "warehouse.device_status", Service: "warehousecore", Input: DeviceStatusUpdateInput{}, Required: []string{"device_id", "status|condition_status"}, Operations: []string{"prepare_update", "update"}},
		"procurement.products":      {Entity: "procurement.products", Service: "procurementcore", Input: ProductCreateInput{}, Required: []string{"sku", "name", "category_id"}, Operations: []string{"prepare_create", "create"}},
		"procurement.orders":        {Entity: "procurement.orders", Service: "procurementcore", Input: PurchaseOrderCreateInput{}, Required: []string{"supplier_id|supplier_query", "lines"}, Operations: []string{"prepare_create", "create"}},
		"planner.plans":             {Entity: "planner.plans", Service: "plannercore", Input: PlannerPlanCreateInput{}, Required: []string{"name"}, Operations: []string{"prepare_create", "create"}},
		"planner.tasks":             {Entity: "planner.tasks", Service: "plannercore", Input: PlannerTaskCreateInput{}, Required: []string{"plan_id", "title"}, Operations: []string{"prepare_create", "create"}},
	}
}

func renderWritableEntitySchema(schema writableEntitySchema) map[string]any {
	required := map[string]bool{}
	requiredGroups := map[string]string{}
	for _, field := range schema.Required {
		if strings.Contains(field, "|") {
			for _, alternative := range strings.Split(field, "|") {
				requiredGroups[alternative] = field
			}
		} else {
			required[field] = true
		}
	}
	typeOf := reflect.TypeOf(schema.Input)
	fields := make([]map[string]any, 0, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
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
		fields = append(fields, entry)
	}
	return map[string]any{"entity": schema.Entity, "service": schema.Service, "operations": schema.Operations, "required": schema.Required, "fields": fields, "notes": schema.Notes}
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

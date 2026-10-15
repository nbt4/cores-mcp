package mcpserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const rentalFinancialScope = "cores:rental:financial"
const procurementFinancialScope = "cores:procurement:financial"

// Set by Store.Query from the explicitly granted scope inside each read-only
// transaction. Price-ranked offer identities must not leak via nonfinancial reads.
const procurementFinancialReadAccess = `(current_setting('cores.procurement_financial', true)='true')`

func hasFinancialScope(ctx context.Context, scope string) bool {
	info := auth.TokenInfoFromContext(ctx)
	return info != nil && containsString(info.Scopes, scope)
}

var financialOnlyReadTools = map[string]string{
	"rental.revenue.summary":                     rentalFinancialScope,
	"rental.jobs.get":                            rentalFinancialScope,
	"rental.job_positions.get":                   rentalFinancialScope,
	"rental.job_positions.search":                rentalFinancialScope,
	"warehouse.maintenance_orders.financial_get": warehouseFinancialScope,
	"procurement.offers.compare":                 procurementFinancialScope,
	"procurement.prices.history":                 procurementFinancialScope,
	"procurement.spend.summary":                  procurementFinancialScope,
}

func requireFinancialReadToolScope(ctx context.Context, tool string) error {
	if scope := financialOnlyReadTools[tool]; scope != "" && !hasFinancialScope(ctx, scope) {
		return fmt.Errorf("explicit %s scope required; reconnect and separately grant the requested financial access. Read-only financial access never grants writes", scope)
	}
	return nil
}

// Only these dedicated read tools return the financial fields below. Discovery
// metadata, validated flexible query aliases and mutation receipts are separate
// contracts and must not be rewritten by a result projection.
var financialMixedReadTools = map[string]bool{
	"rental.jobs.search": true, "rental.jobs.get": true, "rental.external_equipment.list": true,
	"warehouse.products.search": true, "warehouse.products.get": true, "warehouse.devices.get": true,
	"warehouse.packages.search": true, "warehouse.maintenance.due": true, "warehouse.utilization.summary": true,
	"procurement.products.search": true, "procurement.products.get": true, "procurement.suppliers.search": true,
	"procurement.requisitions.list": true, "procurement.orders.list": true, "procurement.deliveries.expected": true,
	"procurement.reorder.candidates": true, "procurement.audit.history": true,
	"procurement.suppliers.audit_history": true, "procurement.products.audit_history": true, "procurement.offers.audit_history": true,
	"procurement.categories.audit_history": true, "procurement.orders.audit_history": true, "procurement.requisitions.audit_history": true,
	"inventory.alternatives.compare": true, "inventory.procurement.recommendations": true, "equipment.strategy.context": true,
}

var financialReadFields = map[string]string{
	"price_per_unit": warehouseFinancialScope, "itemcostperday": warehouseFinancialScope,
	"total_revenue": warehouseFinancialScope, "last_maintenance_cost": warehouseFinancialScope,
	"cost": warehouseFinancialScope, "price": warehouseFinancialScope, "asset_revenue": warehouseFinancialScope,
	"recorded_maintenance_cost": warehouseFinancialScope, "rental_price": rentalFinancialScope, "customer_price": rentalFinancialScope,
	"revenue": rentalFinancialScope, "final_revenue": rentalFinancialScope, "discount": rentalFinancialScope,
	"discount_type": rentalFinancialScope, "multiply_by_days": rentalFinancialScope, "prices_include_tax": rentalFinancialScope,
	"custom_price": rentalFinancialScope, "total_cost": rentalFinancialScope, "planned_revenue": rentalFinancialScope,
	"historic_cost": rentalFinancialScope,
	"unit_price":    rentalFinancialScope, "follow_day_factor": rentalFinancialScope, "discount_percent": rentalFinancialScope, "discount_amount": rentalFinancialScope, "tax_rate": rentalFinancialScope,
	"price_cents": procurementFinancialScope, "price_per_unit_cents": procurementFinancialScope,
	"best_price_cents": procurementFinancialScope, "best_current_price_cents": procurementFinancialScope,
	"estimated_price_cents": procurementFinancialScope, "estimated_total_cents": procurementFinancialScope,
	"unit_price_cents": procurementFinancialScope, "total_cents": procurementFinancialScope,
	"ordered_total_cents": procurementFinancialScope, "change_cents": procurementFinancialScope,
	"price_cents_before": procurementFinancialScope, "price_cents_after": procurementFinancialScope,
	"total_cents_before": procurementFinancialScope, "total_cents_after": procurementFinancialScope,
}

func projectFinancialReadData(ctx context.Context, tool string, value any) (any, []string) {
	if !financialMixedReadTools[tool] {
		return value, nil
	}
	omitted := map[string]bool{}
	if (tool == "procurement.products.get" || tool == "procurement.reorder.candidates" || tool == "inventory.alternatives.compare" || tool == "inventory.procurement.recommendations") && !hasFinancialScope(ctx, procurementFinancialScope) {
		omitted[procurementFinancialScope] = true
	}
	var project func(any) any
	project = func(value any) any {
		switch typed := value.(type) {
		case map[string]any:
			result := make(map[string]any, len(typed))
			for key, child := range typed {
				if scope := financialReadFields[key]; scope != "" && !hasFinancialScope(ctx, scope) {
					omitted[scope] = true
					continue
				}
				result[key] = project(child)
			}
			return result
		case []map[string]any:
			if typed == nil {
				return typed
			}
			result := make([]map[string]any, len(typed))
			for i, child := range typed {
				result[i] = project(child).(map[string]any)
			}
			return result
		case []any:
			if typed == nil {
				return typed
			}
			result := make([]any, len(typed))
			for i, child := range typed {
				result[i] = project(child)
			}
			return result
		default:
			return value
		}
	}
	result := project(value)
	scopes := make([]string, 0, len(omitted))
	for scope := range omitted {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	warnings := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		warnings = append(warnings, "Financial fields and any price-ranked offer selection omitted: explicit "+scope+" access was not granted. Operational quantities and dates remain available.")
	}
	return result, warnings
}

func checkFinancialQuery(ctx context.Context, entity string, fields []string) error {
	spec := queryEntities[entity]
	for _, field := range fields {
		if _, exists := spec.Fields[field]; !exists {
			continue
		}
		if scope := financialReadFields[field]; scope != "" && !hasFinancialScope(ctx, scope) {
			return fmt.Errorf("field %s.%s requires explicit %s for projection, filters, sorting, joins or aggregation", entity, field, scope)
		}
	}
	return nil
}

func financialQueryDefaults(ctx context.Context, spec queryEntitySpec) queryEntitySpec {
	fields := make([]string, 0, len(spec.DefaultFields))
	for _, field := range spec.DefaultFields {
		if scope := financialReadFields[field]; scope == "" || hasFinancialScope(ctx, scope) {
			fields = append(fields, field)
		}
	}
	spec.DefaultFields = fields
	return spec
}

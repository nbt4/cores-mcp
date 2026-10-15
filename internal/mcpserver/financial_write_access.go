package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// A financial grant is independent of every action grant, including legacy
// cores:write. Check it before preparation, dry-run and any cached receipt.
func procurementFinancialWriteTool(name string) bool {
	name = executionToolForPreparation(name)
	return isMutationTool(name) && (strings.HasPrefix(name, "procurement.offers.") || strings.HasPrefix(name, "procurement.requisitions.") || strings.HasPrefix(name, "procurement.orders."))
}

func procurementProductFinancialInput(input any) bool {
	raw, err := json.Marshal(input)
	if err != nil {
		return true
	}
	var fields struct {
		ProductURL string `json:"product_url"`
		SupplierID int64  `json:"supplier_id"`
		PriceCents int64  `json:"price_cents"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return true
	}
	// URL analysis can discover a price even when the caller did not supply one.
	return strings.TrimSpace(fields.ProductURL) != "" || fields.SupplierID != 0 || fields.PriceCents != 0
}

func requireProcurementFinancialWrite(ctx context.Context, name string, input any) error {
	name = executionToolForPreparation(name)
	required := procurementFinancialWriteTool(name) || name == "procurement.products.create" && procurementProductFinancialInput(input)
	if required && !hasFinancialScope(ctx, procurementFinancialScope) {
		return fmt.Errorf("explicit %s scope required for financial preparation, execution and replay; reconnect and separately grant Procurement financial access", procurementFinancialScope)
	}
	return nil
}

package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

type RequisitionOrderInput struct {
	MutationControl
	RequisitionID     int64   `json:"requisition_id"`
	SupplierID        int64   `json:"supplier_id"`
	ExpectedDelivery  *string `json:"expected_delivery,omitempty" jsonschema:"Optional RFC3339 timestamp."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact requisition version from prepare_order."`
	ExpectedContext   string  `json:"expected_context,omitempty" jsonschema:"Complete final requisition, supplier, offer and order draft context from prepare_order."`
	ConfirmationText  string  `json:"confirmation_text,omitempty" jsonschema:"Exact final requisition/supplier/context-bound phrase."`
	ConfirmCreation   bool    `json:"confirm_creation,omitempty"`
}

func isProcurementRequisitionOrderTool(name string) bool {
	return name == "procurement.requisitions.order"
}

func registerProcurementRequisitionOrderTools(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "procurement.requisitions.prepare_order", "Prepare order from approved requisition", "Review the original approved requisition, selected supplier, complete offer candidates and derived order draft. No external submission or stock movement.", func(ctx context.Context, input RequisitionOrderInput) (any, []Source, []string, error) {
		return invokeProcurementRequisitionOrder(ctx, cfg, input, true)
	})
	addUpdateTool(server, "procurement.requisitions.order", "Create order from approved requisition", "Atomically create exactly one reviewed supplier order draft and mark the original approved requisition ordered. Current administrator/create rights, exact final version/context and bound confirmation required.", func(ctx context.Context, input RequisitionOrderInput) (any, []Source, []string, error) {
		return invokeProcurementRequisitionOrder(ctx, cfg, input, false)
	})
}

func invokeProcurementRequisitionOrder(ctx context.Context, cfg config.Config, input RequisitionOrderInput, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{"id": input.RequisitionID, "supplier_id": input.SupplierID, "expected_updated_at": input.ExpectedUpdatedAt, "expected_context": input.ExpectedContext, "confirmation_text": input.ConfirmationText, "confirm_change": input.ConfirmCreation, "preview": preview || input.DryRun || !input.ConfirmCreation}
	if input.ExpectedDelivery != nil {
		body["expected_delivery"] = *input.ExpectedDelivery
	}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/requisition-orders", http.MethodPost, body, &result)
	sources := []Source{{Service: "procurementcore", Entity: "requisition", ID: fmt.Sprint(input.RequisitionID)}, {Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(input.SupplierID)}}
	if row, ok := result["purchase_order"].(map[string]any); ok {
		sources = append(sources, Source{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(row["id"])})
	}
	if deps, ok := result["dependencies"].(map[string]any); ok {
		for _, spec := range []struct{ key, kind string }{{"products", "product"}, {"offers", "offer"}, {"existing_orders", "purchase_order"}, {"preferred_suppliers", "supplier"}} {
			if rows, ok := deps[spec.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok {
						sources = append(sources, Source{Service: "procurementcore", Entity: spec.kind, ID: fmt.Sprint(row["id"])})
					}
				}
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Current administrator and exact create scope are checked before every cached or durable replay. Copy the complete final requisition version, dependency context and bound phrase. The native cheapest active supplier offer rule retains preferred estimates and original demand quantities; expired offers, incompatible currency, minimum quantities and packs require correction. Original requisition fields and line IDs remain intact. Order, requisition status, both audits/activities and durable response commit together. This creates a draft and sends no supplier message or stock movement."), err
}

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

type OrderDraftUpdateInput struct {
	MutationControl
	OrderID             int64                     `json:"order_id,omitempty"`
	SupplierID          *int64                    `json:"supplier_id,omitempty"`
	SupplierOrderNumber *string                   `json:"supplier_order_number,omitempty"`
	Currency            *string                   `json:"currency,omitempty"`
	OrderDate           *string                   `json:"order_date,omitempty" jsonschema:"RFC3339 timestamp; empty string clears."`
	ExpectedDelivery    *string                   `json:"expected_delivery,omitempty" jsonschema:"RFC3339 timestamp; empty string clears."`
	Notes               *string                   `json:"notes,omitempty"`
	Lines               *[]PurchaseOrderLineInput `json:"lines,omitempty" jsonschema:"Complete replacement line list; optional line_id retains original identities. Omit to retain all original lines and native fields."`
	ExpectedUpdatedAt   string                    `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update."`
	ExpectedContext     string                    `json:"expected_context,omitempty" jsonschema:"Exact full current/draft/reference/duplicate context from prepare_update."`
	ConfirmationText    string                    `json:"confirmation_text,omitempty" jsonschema:"Exact final context-bound draft confirmation phrase."`
	ConfirmUpdate       bool                      `json:"confirm_update,omitempty"`
}

func isProcurementOrderDraftTool(name string) bool {
	return name == "procurement.orders.create" || name == "procurement.orders.update"
}
func registerProcurementOrderDraftTools(server *mcp.Server, cfg config.Config, _ *store.Store) {
	addWritePreparationTool(server, "procurement.orders.prepare_update", "Prepare purchase order draft update", "Review complete original/proposed fields, retained line identities, canonical total, current references, duplicate supplier numbers and exact owner context.", func(ctx context.Context, input OrderDraftUpdateInput) (any, []Source, []string, error) {
		return invokeProcurementOrderDraft(ctx, cfg, "update", input, true)
	})
	addUpdateTool(server, "procurement.orders.update", "Update purchase order draft", "Update an unreceived ordinary draft through the atomic owning workflow. Omitted lines retain original IDs; exact final context and current administrator/update rights are required.", func(ctx context.Context, input OrderDraftUpdateInput) (any, []Source, []string, error) {
		return invokeProcurementOrderDraft(ctx, cfg, "update", input, false)
	})
}
func invokeProcurementOrderDraft(ctx context.Context, cfg config.Config, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	var id int64
	body := map[string]any{}
	copyLines := func(lines []PurchaseOrderLineInput) []map[string]any {
		out := make([]map[string]any, 0, len(lines))
		for _, l := range lines {
			line := map[string]any{"line_id": l.LineID, "description": l.Description, "quantity": l.Quantity, "unit": l.Unit, "unit_price_cents": l.UnitPriceCents, "purchase_url": l.PurchaseURL}
			if l.ProductID != 0 {
				line["product_id"] = l.ProductID
			}
			out = append(out, line)
		}
		return out
	}
	switch in := input.(type) {
	case PurchaseOrderCreateInput:
		if op != "create" {
			return nil, nil, nil, fmt.Errorf("closed creation input required")
		}
		body = map[string]any{"status": strings.ToLower(strings.TrimSpace(in.Status)), "supplier_query": in.SupplierQuery, "supplier_order_number": in.SupplierOrderNumber, "currency": in.Currency, "order_date": in.OrderDate, "expected_delivery": in.ExpectedDelivery, "notes": in.Notes, "lines": copyLines(in.Lines), "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": in.ConfirmCreation, "preview": preview || in.DryRun || !in.ConfirmCreation}
		if in.SupplierID != 0 {
			body["supplier_id"] = in.SupplierID
		}
	case OrderDraftUpdateInput:
		if op != "update" {
			return nil, nil, nil, fmt.Errorf("closed update input required")
		}
		id = in.OrderID
		body = map[string]any{"id": id, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": in.ConfirmUpdate, "preview": preview || in.DryRun || !in.ConfirmUpdate}
		if in.SupplierID != nil {
			body["supplier_id"] = *in.SupplierID
		}
		if in.Lines != nil {
			body["lines"] = copyLines(*in.Lines)
		}
		for _, f := range []struct {
			key   string
			value *string
		}{{"supplier_order_number", in.SupplierOrderNumber}, {"currency", in.Currency}, {"order_date", in.OrderDate}, {"expected_delivery", in.ExpectedDelivery}, {"notes", in.Notes}} {
			if f.value != nil {
				body[f.key] = *f.value
			}
		}
	default:
		return nil, nil, nil, fmt.Errorf("closed order draft input required")
	}
	result := map[string]any{}
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(id)}}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/order-drafts/"+op, http.MethodPost, body, &result)
	if row, ok := result["purchase_order"].(map[string]any); ok {
		sources[0].ID = fmt.Sprint(row["id"])
	}
	if deps, ok := result["dependencies"].(map[string]any); ok {
		for _, spec := range []struct{ key, kind string }{{"products", "product"}, {"suppliers", "supplier"}, {"requisition", "requisition"}, {"receipts", "goods_receipt"}, {"supplier_confirmations", "order_confirmation"}} {
			if rows, ok := deps[spec.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok {
						sources = append(sources, Source{Service: "procurementcore", Entity: spec.kind, ID: fmt.Sprint(row["id"])})
					}
				}
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Current administrator/action rights are rechecked before cached, legacy and restarted replay. Copy exact final context/version and bound phrase. Omitted lines retain identities; explicit replacements show the complete diff. Confirmed/received and Amazon drafts require their separate original workflows. Drafts do not send external orders or change stock. Change, native audit/activity and durable response commit atomically. Old successful native business results may replay; unbooked legacy requests must be prepared again."), err
}

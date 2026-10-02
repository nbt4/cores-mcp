package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nbt4/cores-mcp/internal/config"
)

func isProcurementApprovalTool(name string) bool {
	return name == "procurement.orders.transition" || name == "procurement.requisitions.decide"
}
func invokeProcurementApproval(ctx context.Context, cfg config.Config, entity string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{}
	var id int64
	switch in := input.(type) {
	case RequisitionDecisionInput:
		id = in.RequisitionID
		body = map[string]any{"id": id, "decision": in.Decision, "note": in.Note, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": in.ConfirmDecision, "preview": preview || in.DryRun || !in.ConfirmDecision}
	case OrderTransitionInput:
		id = in.OrderID
		body = map[string]any{"id": id, "status": in.Status, "reason": in.Reason, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": in.ConfirmTransition, "preview": preview || in.DryRun || !in.ConfirmTransition}
	default:
		return nil, nil, nil, fmt.Errorf("closed procurement approval input required")
	}
	sources := []Source{{Service: "procurementcore", Entity: map[string]string{"orders": "purchase_order", "requisitions": "requisition"}[entity], ID: fmt.Sprint(id)}}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/approvals/"+entity, http.MethodPost, body, &result)
	if deps, ok := result["dependencies"].(map[string]any); ok {
		for _, spec := range []struct{ key, kind string }{{"products", "product"}, {"suppliers", "supplier"}, {"supplier", "supplier"}, {"requisition", "requisition"}, {"receipts", "goods_receipt"}} {
			if rows, ok := deps[spec.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok {
						sources = append(sources, Source{Service: "procurementcore", Entity: spec.kind, ID: fmt.Sprint(row["id"])})
					}
				}
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Explicit approval scope, current administrator and exact final record/line/reference/action context are required. Requisition decisions require a different user than the original requester, including replay. Audits, native activity and durable response commit atomically. Sent records business status and does not send supplier messages. Cancellation preserves received stock/receipts and separate putaway obligations. Old successful native approvals may replay their original saved business response; new requests require the context-bound owner preview."), err
}

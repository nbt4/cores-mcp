package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type OrderTransitionInput struct {
	MutationControl
	OrderID           int64  `json:"order_id,omitempty" jsonschema:"Exact ProcurementCore purchase order ID."`
	Status            string `json:"status,omitempty" jsonschema:"Next status: sent, confirmed, or cancelled."`
	Reason            string `json:"reason,omitempty" jsonschema:"Required cancellation reason added to order notes."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact owning-Core fingerprint of record/lines, references, receipts, reviewed status and reason."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_transition."`
	ConfirmTransition bool   `json:"confirm_transition,omitempty"`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Record-bound phrase from prepare_transition."`
}

func registerProcurementOrderTransitionTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	addWritePreparationTool(server, "procurement.orders.prepare_transition", "Prepare order status transition", "Review the complete record/lines, references, receipts and exact status/reason-bound owner confirmation without mutation.", func(ctx context.Context, input OrderTransitionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "orders", input, true)
	})
	addUpdateTool(server, "procurement.orders.transition", "Transition purchase order", "Send, confirm or cancel through the closed owner API after explicit approval scope, current administrator, exact record/context and elevated confirmation. Native audit/activity/durable replay commit together; physical stock and external messages are preserved.", func(ctx context.Context, input OrderTransitionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "orders", input, false)
	})
}

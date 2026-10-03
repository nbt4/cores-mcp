package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

type SupplierSubmissionReconcileInput struct {
	MutationControl
	OrderID              int64  `json:"order_id"`
	Resolution           string `json:"resolution" jsonschema:"Human-verified found_order or confirmed_not_sent; never infer a negative result from a timeout."`
	SupplierOrderNumber  string `json:"supplier_order_number,omitempty"`
	VerificationEvidence string `json:"verification_evidence,omitempty" jsonschema:"Business evidence of the completed human check in the supplier account, 10-2000 characters. Exclude credentials, payment and unnecessary personal data."`
	HumanVerified        bool   `json:"human_verified,omitempty" jsonschema:"True only after a human has checked the original order, supplier account, payload identity and supplier response."`
	ExpectedUpdatedAt    string `json:"expected_updated_at,omitempty"`
	ExpectedContext      string `json:"expected_context,omitempty"`
	ConfirmationText     string `json:"confirmation_text,omitempty"`
	ConfirmReconcile     bool   `json:"confirm_reconcile,omitempty"`
}

func registerProcurementSubmissionReconcileTools(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "procurement.orders.prepare_reconcile_submission", "Review human supplier verification", "Review the original immutable supplier claim, uncertain outcome, full order and human verification evidence without supplier requests or data changes. Current administrator and explicit send scope required; wait at least 15 minutes after the original claim.", func(ctx context.Context, input SupplierSubmissionReconcileInput) (any, []Source, []string, error) {
		return invokeProcurementSubmissionReconcile(ctx, cfg, input, true)
	})
	addUpdateTool(server, "procurement.orders.reconcile_submission", "Record human supplier verification", "After a completed human supplier-account check, record found_order with its supplier reference or confirmed_not_sent with evidence. Retain the original claim and acknowledgement, never resend or reopen this order. Current admin/send rights, exact final version/context, elevated phrase and confirmation required.", func(ctx context.Context, input SupplierSubmissionReconcileInput) (any, []Source, []string, error) {
		return invokeProcurementSubmissionReconcile(ctx, cfg, input, false)
	})
}

func invokeProcurementSubmissionReconcile(ctx context.Context, cfg config.Config, input SupplierSubmissionReconcileInput, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{"id": input.OrderID, "resolution": input.Resolution, "supplier_order_number": input.SupplierOrderNumber, "verification_evidence": input.VerificationEvidence, "human_verified": input.HumanVerified, "expected_updated_at": input.ExpectedUpdatedAt, "expected_context": input.ExpectedContext, "confirmation_text": input.ConfirmationText, "confirm_change": input.ConfirmReconcile, "preview": preview || input.DryRun || !input.ConfirmReconcile}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/orders/reconcile-submission", http.MethodPost, body, &result)
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(input.OrderID)}}
	if _, ok := result["submission_id"]; !ok {
		if submission, ok := result["submission"].(map[string]any); ok {
			if id, ok := submission["id"]; ok {
				sources = append(sources, Source{Service: "procurementcore", Entity: "order_submission", ID: fmt.Sprint(id)})
			}
		}
	}
	for _, field := range []struct{ key, entity string }{{"submission_id", "order_submission"}, {"reconciliation_id", "submission_reconciliation"}} {
		if id, ok := result[field.key]; ok {
			sources = append(sources, Source{Service: "procurementcore", Entity: field.entity, ID: fmt.Sprint(id)})
		}
	}
	return result, sources, append(untrustedTextWarning(), "This tool records a human-verified supplier outcome; it cannot prove non-submission from a timeout, retry, empty search or AI inference. Check the correct supplier account, original order and payload identity, then document business evidence and explicitly confirm the exact resolution. No supplier requests, new orders, automatic resend or stock changes occur. The original claim/acknowledgement stays immutable. A found order becomes sent; confirmed non-submission cancels this order permanently. Any new demand requires a separately approved supplier cart. Current administrator and explicit supplier send rights apply to every replay."), err
}

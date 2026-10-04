package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

type AmazonOrderSendInput struct {
	MutationControl
	OrderID           int64  `json:"order_id"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact order version from prepare_send_amazon."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Complete final order, approved source, supplier, destinations, mode and price context."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact order, EUR total and context-bound supplier send phrase."`
	ConfirmSend       bool   `json:"confirm_send,omitempty"`
}

func isProcurementSupplierSendTool(name string) bool {
	return name == "procurement.orders.send_amazon" || name == "procurement.orders.reconcile_submission" || name == "procurement.orders.build_adam_hall_cart" || name == "procurement.orders.send_adam_hall"
}

func registerProcurementAmazonSendTools(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "procurement.orders.prepare_send_amazon", "Prepare Amazon supplier submission", "Review complete original order, references, supplier payload, exact EUR total, business destinations and deployment mode without sending or changing data. Explicit supplier send scope required.", func(ctx context.Context, input AmazonOrderSendInput) (any, []Source, []string, error) {
		return invokeProcurementAmazonSend(ctx, cfg, input, true)
	})
	addUpdateTool(server, "procurement.orders.send_amazon", "Send approved Amazon order", "Send the exact confirmed Amazon Business order once through the durable owner claim. Current administrator and explicit supplier send scope, exact final version/context and paid-order phrase required. Uncertain outcomes are never automatically resubmitted.", func(ctx context.Context, input AmazonOrderSendInput) (any, []Source, []string, error) {
		return invokeProcurementAmazonSend(ctx, cfg, input, false)
	})
}

func invokeProcurementAmazonSend(ctx context.Context, cfg config.Config, input AmazonOrderSendInput, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{"id": input.OrderID, "expected_updated_at": input.ExpectedUpdatedAt, "expected_context": input.ExpectedContext, "confirmation_text": input.ConfirmationText, "confirm_change": input.ConfirmSend, "preview": preview || input.DryRun || !input.ConfirmSend}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/orders/send-amazon", http.MethodPost, body, &result)
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(input.OrderID)}}
	if id, ok := result["submission_id"]; ok {
		sources = append(sources, Source{Service: "procurementcore", Entity: "order_submission", ID: fmt.Sprint(id)})
	}
	return result, sources, append(untrustedTextWarning(), "This workflow sends a paid supplier order. Legacy write/create/update/approve/submit access does not grant supplier send. Show the complete final order, EUR total, addresses and mode, then copy its exact version/context and elevated phrase. Current administrator/send rights apply to every cached and durable replay. A committed unique claim precedes the external call. Retries may finalize saved acknowledgement but never send again. Pending or unknown outcomes require reconciliation in Amazon Business. Local data and supplier acknowledgements cannot share a database transaction; durable phases and audits preserve the outcome and prevent duplicates. Receiving and stock movements remain separate."), err
}

package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

type AdamHallOrderControl struct {
	OrderID           int64  `json:"order_id"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
	ExpectedContext   string `json:"expected_context,omitempty"`
	ConfirmationText  string `json:"confirmation_text,omitempty"`
}
type AdamHallCartInput struct {
	MutationControl
	AdamHallOrderControl
	ConfirmCart bool `json:"confirm_cart,omitempty"`
}
type AdamHallSendInput struct {
	MutationControl
	AdamHallOrderControl
	CheckoutID  int64 `json:"checkout_id,omitempty" jsonschema:"Exact ready checkout ID from the final paid order preview."`
	ConfirmSend bool  `json:"confirm_send,omitempty"`
}

func registerProcurementAdamHallTools(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "procurement.orders.prepare_build_adam_hall_cart", "Review Adam Hall cart preparation", "Review the complete local draft, supplier product numbers, references, account fingerprint and remote cart effects without supplier requests or data changes. Explicit send/current administrator rights required.", func(ctx context.Context, in AdamHallCartInput) (any, []Source, []string, error) {
		return invokeProcurementAdamHall(ctx, cfg, "cart", in.AdamHallOrderControl, 0, false, true)
	})
	addUpdateTool(server, "procurement.orders.build_adam_hall_cart", "Prepare confirmed Adam Hall cart", "After exact complete draft/version/context and cart-specific confirmation, prepare the remote supplier cart and persist its reviewed prices, business destinations and methods. This changes the supplier cart but never places a paid order; existing unrelated cart contents are never removed.", func(ctx context.Context, in AdamHallCartInput) (any, []Source, []string, error) {
		return invokeProcurementAdamHall(ctx, cfg, "cart", in.AdamHallOrderControl, 0, in.ConfirmCart, in.DryRun)
	})
	addWritePreparationTool(server, "procurement.orders.prepare_send_adam_hall", "Review paid Adam Hall order", "Review the exact fresh saved supplier checkout, business delivery/billing addresses, payment/shipping methods, EUR total and proposed local prices without supplier requests or changes. Explicit send/current administrator rights required.", func(ctx context.Context, in AdamHallSendInput) (any, []Source, []string, error) {
		return invokeProcurementAdamHall(ctx, cfg, "send", in.AdamHallOrderControl, in.CheckoutID, false, true)
	})
	addUpdateTool(server, "procurement.orders.send_adam_hall", "Send reviewed Adam Hall order", "Send the exact confirmed supplier checkout once through the durable owner claim. Require current admin/explicit send rights, exact checkout/version/context, elevated paid-order phrase and confirmation. Revalidate the same private cart; never rebuild or silently accept changed prices/destinations. Uncertain outcomes are never resent.", func(ctx context.Context, in AdamHallSendInput) (any, []Source, []string, error) {
		return invokeProcurementAdamHall(ctx, cfg, "send", in.AdamHallOrderControl, in.CheckoutID, in.ConfirmSend, in.DryRun)
	})
}

func invokeProcurementAdamHall(ctx context.Context, cfg config.Config, operation string, in AdamHallOrderControl, checkoutID int64, confirmed, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{"id": in.OrderID, "checkout_id": checkoutID, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": confirmed, "preview": preview || !confirmed}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/orders/adam-hall/"+operation, http.MethodPost, body, &result)
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(in.OrderID)}}
	for _, field := range []struct{ key, entity string }{{"checkout_id", "adam_hall_checkout"}, {"submission_id", "order_submission"}} {
		if id, ok := result[field.key]; ok && id != float64(0) {
			sources = append(sources, Source{Service: "procurementcore", Entity: field.entity, ID: fmt.Sprint(id)})
		}
	}
	warning := "Cart preparation is an explicitly confirmed remote write; it is separate from a paid order. Preparation/dry-run tools perform no supplier calls. Unrelated cart contents are retained. Current administrator and explicit send rights, exact full record/reference context and the cart phrase apply to execution and every replay. The private supplier context is encrypted in the owner and never returned or audited. A saved ready checkout provides the business prices and destinations for a separate paid-order confirmation."
	if operation == "send" {
		warning = "This sends a paid supplier order. Show the full fresh checkout, delivery/billing addresses, methods, EUR total and proposed local prices, then copy exact checkout/version/context and paid-order phrase. The same private cart is revalidated; changed content/prices/methods stop checkout. A committed immutable claim precedes any external order request. Retries only return/finalize saved outcomes and never resend. Pending or uncertain results require human verification in the supplier account. Local DB and supplier checkout cannot share one transaction; durable audited phases retain the outcome. Stock/receiving remain separate."
	}
	return result, sources, append(untrustedTextWarning(), warning), err
}

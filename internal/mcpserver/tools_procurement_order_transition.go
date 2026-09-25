package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type OrderTransitionInput struct {
	MutationControl
	OrderID           int64  `json:"order_id,omitempty" jsonschema:"Exact ProcurementCore purchase order ID."`
	Status            string `json:"status,omitempty" jsonschema:"Next status: sent, confirmed, or cancelled."`
	Reason            string `json:"reason,omitempty" jsonschema:"Required cancellation reason added to order notes."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_transition."`
	ConfirmTransition bool   `json:"confirm_transition,omitempty"`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Record-bound phrase from prepare_transition."`
}

func registerProcurementOrderTransitionTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.orders.prepare_transition", "Prepare order status transition", "Load the order and every line, check the allowed transition, show the complete status diff, exact version and confirmation phrase.", func(ctx context.Context, input OrderTransitionInput) (any, []Source, []string, error) {
		p, err := prepareOrderTransition(ctx, db, input)
		return p.response("draft"), orderTransitionSources(input.OrderID, p), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.orders.transition", "Transition purchase order", "Send, confirm or cancel one order after approval scope, exact version, full preview and elevated confirmation.", func(ctx context.Context, input OrderTransitionInput) (any, []Source, []string, error) {
		p, err := prepareOrderTransition(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), orderTransitionSources(input.OrderID, p), nil, err
		}
		if !input.ConfirmTransition {
			return p.response("confirmation_required"), orderTransitionSources(input.OrderID, p), []string{"No data was changed."}, nil
		}
		if strings.TrimSpace(input.ConfirmationText) != orderTransitionPhrase(input.Status, input.OrderID) {
			return p.response("elevated_confirmation_required"), orderTransitionSources(input.OrderID, p), []string{"No data was changed. Type the phrase shown by prepare_transition."}, nil
		}
		var result map[string]any
		payload := cloneMap(p.Draft)
		delete(payload, "confirmation_text_required")
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/orders/%d", input.OrderID), http.MethodPut, payload, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "transitioned", "purchase_order": result, "diff": p.Diff}, orderTransitionSources(input.OrderID, p), p.Warnings, nil
	})
}

func orderTransitionPhrase(status string, id int64) string {
	verb := map[string]string{"sent": "SEND", "confirmed": "CONFIRM", "cancelled": "CANCEL"}[strings.ToLower(strings.TrimSpace(status))]
	if verb == "" || id <= 0 {
		return ""
	}
	return fmt.Sprintf("%s ORDER %d", verb, id)
}

func orderTransitionSources(id int64, p preparedMutation) []Source {
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(id)}}
	if supplierID := numericID(p.Current["supplier_id"]); supplierID > 0 {
		sources = append(sources, Source{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(supplierID)})
	}
	return sources
}

func prepareOrderTransition(ctx context.Context, db *store.Store, input OrderTransitionInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to change purchase order status")
	}
	if input.OrderID <= 0 {
		p.require("order_id", "Welche Bestellung soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT id,number,status,supplier_id,supplier_order_number,expected_delivery,notes,total_cents,to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_purchase_orders WHERE id=$1`, input.OrderID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("order_id", "Bestellung wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	from := fmt.Sprint(rows[0]["status"])
	to := strings.ToLower(strings.TrimSpace(input.Status))
	allowed := map[string][]string{"draft": {"sent", "cancelled"}, "sent": {"confirmed", "cancelled"}, "confirmed": {"cancelled"}, "partially_received": {"cancelled"}}
	if !containsString(allowed[from], to) {
		p.require("status", fmt.Sprintf("Von %s aus ist dieser Statuswechsel nicht möglich.", from), allowed[from])
	}
	if to == "cancelled" && strings.TrimSpace(input.Reason) == "" {
		p.require("reason", "Warum soll die Bestellung storniert werden?", nil)
	}
	version := rfc3339Value(rows[0]["updated_at"])
	if input.ConfirmTransition && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Die Version aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Bestellung wurde seit der Vorschau geändert.", version)
	}
	lines, err := db.Query(ctx, `SELECT id AS line_id,product_id,description,quantity,received_quantity,unit,unit_price_cents FROM proc_purchase_order_lines WHERE purchase_order_id=$1 ORDER BY id`, input.OrderID)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = lines
	notes := nullableText(rows[0]["notes"])
	if to == "cancelled" && strings.TrimSpace(input.Reason) != "" {
		if notes != "" {
			notes += "\n"
		}
		notes += "Cancellation: " + strings.TrimSpace(input.Reason)
	}
	p.Draft = map[string]any{"status": to, "supplierOrderNumber": nullableText(rows[0]["supplier_order_number"]), "expectedDelivery": rows[0]["expected_delivery"], "notes": notes, "expectedUpdatedAt": version}
	p.Diff = map[string]map[string]any{"status": {"before": from, "after": to}}
	if to == "cancelled" {
		p.Diff["notes"] = map[string]any{"before": rows[0]["notes"], "after": notes}
	}
	p.Warnings = append(p.Warnings, "This transition is audit-logged. It does not send an order to the supplier; sent records the business status.")
	if to == "cancelled" {
		p.Warnings = append(p.Warnings, "Cancellation preserves received stock and historical receipts; verify remaining supplier obligations separately.")
	}
	p.Draft["confirmation_text_required"] = orderTransitionPhrase(to, input.OrderID)
	p.finish()
	return p, nil
}

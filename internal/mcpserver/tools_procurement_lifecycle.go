package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RequisitionDecisionInput struct {
	MutationControl
	RequisitionID     int64  `json:"requisition_id,omitempty" jsonschema:"Exact submitted ProcurementCore requisition ID."`
	Decision          string `json:"decision,omitempty" jsonschema:"One of approved, rejected, or returned."`
	Note              string `json:"note,omitempty" jsonschema:"Decision note; required when returning a requisition."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact updated_at value returned by prepare_decide; execution is rejected if the requisition changed."`
	ConfirmDecision   bool   `json:"confirm_decision,omitempty" jsonschema:"Set true only after a different authorized user reviewed the complete decision preview."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"For confirmed execution, type the exact confirmation phrase returned by prepare_decide."`
}

type PurchaseOrderReceiptInput struct {
	MutationControl
	OrderID           int64    `json:"order_id,omitempty" jsonschema:"Exact ProcurementCore purchase order ID."`
	LineID            int64    `json:"line_id,omitempty" jsonschema:"Exact line ID belonging to the purchase order."`
	Quantity          float64  `json:"quantity,omitempty" jsonschema:"Positive receipt quantity; exceeding outstanding amount requires explicit allow_overdelivery and the owner overdelivery phrase."`
	Note              string   `json:"note,omitempty"`
	SerialNumbers     []string `json:"serial_numbers,omitempty" jsonschema:"For individually tracked products, exactly one unique serial number per received device."`
	TargetZoneID      *int64   `json:"target_zone_id,omitempty" jsonschema:"Optional active, available, storable WarehouseCore destination zone for the generated putaway task."`
	AllowOverdelivery bool     `json:"allow_overdelivery,omitempty" jsonschema:"Set true only after the preview explicitly identifies and quantifies an intentional overdelivery."`
	ExpectedContext   string   `json:"expected_context,omitempty" jsonschema:"Exact owning-Core fingerprint of the reviewed line, mapping, stock, serials, destination, quantity and order state."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact order updated_at value returned by prepare_receive; execution is rejected if the order changed."`
	ConfirmReceipt    bool     `json:"confirm_receipt,omitempty" jsonschema:"Set true only after reviewing quantity, inventory effects, and order status."`
	ConfirmationText  string   `json:"confirmation_text,omitempty" jsonschema:"For confirmed execution, type the exact confirmation phrase returned by prepare_receive."`
}

func registerProcurementLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)

	addWritePreparationTool(server, "procurement.requisitions.prepare_decide", "Prepare requisition decision", "Load one submitted requisition, enforce separation of duties, verify its version, and preview approval, rejection, or return without changing data.", func(ctx context.Context, input RequisitionDecisionInput) (any, []Source, []string, error) {
		prepared, err := prepareRequisitionDecision(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "procurementcore", "requisition_decision_draft"), append(untrustedTextWarning(), prepared.Warnings...), err
	})
	addUpdateTool(server, "procurement.requisitions.decide", "Decide procurement requisition", "Approve, reject, or return one submitted requisition. Requires a different authorized user, an unchanged version, explicit confirmation, and the record-bound confirmation phrase from prepare_decide.", func(ctx context.Context, input RequisitionDecisionInput) (any, []Source, []string, error) {
		prepared, err := prepareRequisitionDecision(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "procurementcore", "requisition_decision_draft"), prepared.Warnings, err
		}
		if !input.ConfirmDecision {
			return prepared.response("confirmation_required"), mutationSources(prepared, "procurementcore", "requisition_decision_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		expectedPhrase := requisitionDecisionPhrase(input.Decision, input.RequisitionID)
		if strings.TrimSpace(input.ConfirmationText) != expectedPhrase {
			return prepared.response("elevated_confirmation_required"), mutationSources(prepared, "procurementcore", "requisition_decision_draft"), append(prepared.Warnings, "No data was changed. Type the exact confirmation phrase from the preview."), nil
		}
		if strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
			return prepared.response("version_required"), mutationSources(prepared, "procurementcore", "requisition_decision_draft"), append(prepared.Warnings, "No data was changed. Copy expected_updated_at from the latest preview."), nil
		}
		payload := map[string]any{
			"decision":          strings.ToLower(strings.TrimSpace(input.Decision)),
			"note":              strings.TrimSpace(input.Note),
			"expectedUpdatedAt": strings.TrimSpace(input.ExpectedUpdatedAt),
		}
		var result map[string]any
		path := fmt.Sprintf("/api/v1/requisitions/%d/decision", input.RequisitionID)
		if err := api.doJSON(ctx, cfg.ProcurementURL, path, http.MethodPost, payload, &result); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "decided", "previous": prepared.Current, "requisition": result}, []Source{{Service: "procurementcore", Entity: "requisition", ID: fmt.Sprint(input.RequisitionID)}}, prepared.Warnings, nil
	})

	addWritePreparationTool(server, "procurement.orders.prepare_receive", "Prepare goods receipt", "Review exact owner line/order, current mapping and stock, serials, destination and complete physical effects without mutation.", func(ctx context.Context, input PurchaseOrderReceiptInput) (any, []Source, []string, error) {
		return invokeProcurementGoodsReceipt(ctx, cfg, input, true)
	})
	addUpdateTool(server, "procurement.orders.receive", "Book goods receipt", "Book explicitly confirmed partial/full/overdelivery quantities through the owner with exact version/context, quantity-bound confirmation, current real-user receive/admin rights and atomic stock/device/task/audit/durable replay.", func(ctx context.Context, input PurchaseOrderReceiptInput) (any, []Source, []string, error) {
		return invokeProcurementGoodsReceipt(ctx, cfg, input, false)
	})

}

func prepareRequisitionDecision(ctx context.Context, db *store.Store, input RequisitionDecisionInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.RequisitionID <= 0 {
		p.require("requisition_id", "Welche eingereichte Bedarfsanforderung soll entschieden werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT r.id AS requisition_id,r.number,r.title,r.status,r.requester_id,r.requester_name,r.cost_center,r.justification,r.needed_by,r.estimated_total_cents,to_char(r.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,COUNT(rl.id) AS line_count FROM proc_requisitions r LEFT JOIN proc_requisition_lines rl ON rl.requisition_id=r.id WHERE r.id=$1 GROUP BY r.id`, input.RequisitionID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("requisition_id", "Die Bedarfsanforderung existiert nicht. Welche gültige ID soll verwendet werden?", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	if decision != "approved" && decision != "rejected" && decision != "returned" {
		p.require("decision", "Soll die Anforderung approved, rejected oder returned werden?", []string{"approved", "rejected", "returned"})
	}
	if fmt.Sprint(p.Current["status"]) != "submitted" {
		p.require("status", "Nur eine aktuell eingereichte Anforderung kann entschieden werden.", p.Current["status"])
	}
	if info := auth.TokenInfoFromContext(ctx); info != nil && info.UserID == fmt.Sprint(p.Current["requester_id"]) {
		p.require("separation_of_duties", "Die anfordernde Person darf den eigenen Bedarf nicht entscheiden. Ein anderer berechtigter Nutzer muss bestätigen.", nil)
	}
	if decision == "returned" && strings.TrimSpace(input.Note) == "" {
		p.require("note", "Welche Begründung soll der anfordernden Person für die Rückgabe angezeigt werden?", nil)
	}
	currentVersion := rfc3339Value(p.Current["updated_at"])
	if input.ConfirmDecision && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die aktuelle Version aus der letzten Vorschau muss für die Ausführung übernommen werden.", currentVersion)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != currentVersion {
		p.require("expected_updated_at", "Die Anforderung wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", currentVersion)
	}
	phrase := requisitionDecisionPhrase(decision, input.RequisitionID)
	p.Draft = map[string]any{
		"requisition_id": input.RequisitionID, "decision": decision, "note": strings.TrimSpace(input.Note),
		"expected_updated_at": currentVersion, "confirmation_text_required": phrase,
	}
	p.Warnings = append(p.Warnings, "This decision is audit-logged and cannot be executed by the requester of the same requisition.")
	p.finish()
	return p, nil
}

func requisitionDecisionPhrase(decision string, id int64) string {
	verb := map[string]string{"approved": "APPROVE", "rejected": "REJECT", "returned": "RETURN"}[strings.ToLower(strings.TrimSpace(decision))]
	if verb == "" || id <= 0 {
		return ""
	}
	return verb + " REQUISITION " + strconv.FormatInt(id, 10)
}

func rfc3339Value(value any) string {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if typed != nil {
			return typed.UTC().Format(time.RFC3339Nano)
		}
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05.999999999-07"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return text
}

func numericFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int64:
		return float64(typed)
	case int:
		return float64(typed)
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}

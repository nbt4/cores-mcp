package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RequisitionDecisionInput struct {
	MutationControl
	RequisitionID     int64  `json:"requisition_id,omitempty" jsonschema:"Exact submitted ProcurementCore requisition ID."`
	Decision          string `json:"decision,omitempty" jsonschema:"One of approved, rejected, or returned."`
	Note              string `json:"note,omitempty" jsonschema:"Decision note; required when returning a requisition."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond version from the final owner preview."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact owner fingerprint of complete record/lines, referenced catalog and reviewed decision/note."`
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
	addWritePreparationTool(server, "procurement.requisitions.prepare_decide", "Prepare requisition decision", "Review complete record/lines, catalog references, current distinct-administrator rights and exact decision/note context without mutation.", func(ctx context.Context, input RequisitionDecisionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "requisitions", input, true)
	})
	addUpdateTool(server, "procurement.requisitions.decide", "Decide procurement requisition", "Approve, reject or return through the closed owner API after dedicated approval scope, different current administrator, complete exact version/context and record/context-bound confirmation. Audit/activity/durable response commit together; replay rechecks rights.", func(ctx context.Context, input RequisitionDecisionInput) (any, []Source, []string, error) {
		return invokeProcurementApproval(ctx, cfg, "requisitions", input, false)
	})

	addWritePreparationTool(server, "procurement.orders.prepare_receive", "Prepare goods receipt", "Review exact owner line/order, current mapping and stock, serials, destination and complete physical effects without mutation.", func(ctx context.Context, input PurchaseOrderReceiptInput) (any, []Source, []string, error) {
		return invokeProcurementGoodsReceipt(ctx, cfg, input, true)
	})
	addUpdateTool(server, "procurement.orders.receive", "Book goods receipt", "Book explicitly confirmed partial/full/overdelivery quantities through the owner with exact version/context, quantity-bound confirmation, current real-user receive/admin rights and atomic stock/device/task/audit/durable replay.", func(ctx context.Context, input PurchaseOrderReceiptInput) (any, []Source, []string, error) {
		return invokeProcurementGoodsReceipt(ctx, cfg, input, false)
	})

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

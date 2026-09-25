package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type ProcurementAuditInput struct {
	Entity string `json:"entity" jsonschema:"Exact entity: requisition or purchase_order."`
	ID     int64  `json:"id" jsonschema:"Exact ProcurementCore record ID."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum events, capped at 100."`
}

func registerProcurementAuditTool(server *mcp.Server, db *store.Store) {
	addTool(server, "procurement.audit.history", "Review procurement changes", "Return the audit trail for one requisition or purchase order. Shows actor, time, action, status, total, and receipt quantity while excluding notes, document contents, IP addresses and full raw audit JSON.", func(ctx context.Context, input ProcurementAuditInput) (any, []Source, []string, error) {
		return procurementAuditHistory(ctx, db, input)
	})
}

func procurementAuditHistory(ctx context.Context, db *store.Store, input ProcurementAuditInput) (any, []Source, []string, error) {
	entity := strings.ToLower(strings.TrimSpace(input.Entity))
	if entity != "requisition" && entity != "purchase_order" {
		return nil, nil, nil, fmt.Errorf("entity must be requisition or purchase_order")
	}
	if input.ID <= 0 {
		return nil, nil, nil, fmt.Errorf("a positive record ID is required")
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil {
		return nil, nil, nil, fmt.Errorf("interactive Cores user is required")
	}
	if info.Extra["is_admin"] != true {
		if entity != "requisition" {
			return nil, nil, nil, fmt.Errorf("Procurement administrator permission is required for purchase order history")
		}
		owner, err := db.Query(ctx, `SELECT requester_id FROM proc_requisitions WHERE id=$1`, input.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(owner) != 1 || info.UserID != fmt.Sprint(owner[0]["requester_id"]) {
			return nil, nil, nil, fmt.Errorf("only the requester or a procurement administrator may read requisition history")
		}
	}
	tableEntity := "procurement_requisition"
	if entity == "purchase_order" {
		tableEntity = "procurement_order"
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.Query(ctx, `SELECT id AS audit_id,action,entity_type,entity_id,user_id,timestamp AS changed_at,
			COALESCE(new_values->>'origin',old_values->>'origin','UI') AS origin,
			COALESCE(old_values->>'status',old_values#>>'{order,status}') AS status_before,
			COALESCE(new_values#>>'{requisition,status}',new_values#>>'{purchase_order,status}',new_values#>>'{order,status}') AS status_after,
			COALESCE(old_values->>'estimatedTotalCents',old_values->>'totalCents',old_values#>>'{order,totalCents}') AS total_cents_before,
			COALESCE(new_values#>>'{requisition,estimatedTotalCents}',new_values#>>'{purchase_order,totalCents}',new_values#>>'{order,totalCents}') AS total_cents_after,
			new_values#>>'{receipt,quantity}' AS receipt_quantity
			FROM audit_log WHERE entity_type=$1 AND entity_id=$2 ORDER BY timestamp DESC,id DESC LIMIT $3`, tableEntity, fmt.Sprint(input.ID), limit)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{"entity": entity, "id": input.ID, "events": rows}, []Source{{Service: "procurementcore", Entity: entity, ID: fmt.Sprint(input.ID)}}, nil, nil
}

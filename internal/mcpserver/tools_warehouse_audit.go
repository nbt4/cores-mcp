package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseAuditInput struct {
	ProductID int64 `json:"product_id" jsonschema:"Exact WarehouseCore product ID."`
	Limit     int   `json:"limit,omitempty" jsonschema:"Maximum events, capped at 100."`
}

func registerWarehouseAuditTool(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.audit.history", "Review warehouse product changes", "Return redacted product audit history for Warehouse administrators, including creation, update, archive and restore events. Raw audit JSON, IP addresses and private notes are excluded.", func(ctx context.Context, input WarehouseAuditInput) (any, []Source, []string, error) {
		return warehouseAuditHistory(ctx, db, input)
	})
}

func warehouseAuditHistory(ctx context.Context, db *store.Store, input WarehouseAuditInput) (any, []Source, []string, error) {
	if input.ProductID <= 0 {
		return nil, nil, nil, fmt.Errorf("a positive product ID is required")
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return nil, nil, nil, fmt.Errorf("Warehouse administrator permission is required for product history")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := db.Query(ctx, `SELECT id AS audit_id,action,user_id,timestamp AS changed_at,
		COALESCE(new_values->>'origin','UI') AS origin,
		COALESCE(old_values->>'name',old_values#>>'{before,name}') AS name_before,
		COALESCE(new_values#>>'{after,name}',new_values->>'name') AS name_after,
		COALESCE(old_values->>'lifecycle_status',old_values#>>'{before,lifecycle_status}') AS lifecycle_status_before,
		COALESCE(new_values#>>'{after,lifecycle_status}',new_values->>'lifecycle_status') AS lifecycle_status_after,
		COALESCE(old_values->>'tracking_mode',old_values#>>'{before,tracking_mode}') AS tracking_mode_before,
		COALESCE(new_values#>>'{after,tracking_mode}',new_values->>'tracking_mode') AS tracking_mode_after,
		COALESCE(old_values->>'stock_quantity',old_values#>>'{before,stock_quantity}') AS stock_quantity_before,
		COALESCE(new_values#>>'{after,stock_quantity}',new_values->>'stock_quantity') AS stock_quantity_after,
		new_values->>'archived_devices' AS archived_devices,
		new_values->>'restored_devices' AS restored_devices
		FROM audit_log WHERE entity_type='product' AND entity_id=$1
		ORDER BY timestamp DESC,id DESC LIMIT $2`, fmt.Sprint(input.ProductID), limit)
	if err != nil {
		return nil, nil, nil, err
	}
	return map[string]any{"product_id": input.ProductID, "events": rows}, []Source{{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(input.ProductID)}}, nil, nil
}

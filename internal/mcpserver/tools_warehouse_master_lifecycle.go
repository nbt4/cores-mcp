package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseMasterLifecycleInput struct {
	MutationControl
	ID                int64  `json:"id" jsonschema:"Exact existing manufacturer or brand ID, according to the named tool."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond version returned by prepare_archive or prepare_restore."`
	ConfirmLifecycle  bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after presenting current fields, lifecycle diff and all active/historical dependencies and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound ARCHIVE or RESTORE phrase from preview."`
}

func registerWarehouseMasterLifecycleTools(server *mcp.Server, cfg config.Config) {
	api := newCoreAPIClient(cfg)
	for _, entity := range []string{"manufacturer", "brand"} {
		for _, operation := range []string{"archive", "restore"} {
			e, op := entity, operation
			invoke := func(ctx context.Context, in WarehouseMasterLifecycleInput, preview bool) (any, []Source, []string, error) {
				if err := requireWarehouseMasterAdmin(ctx); err != nil {
					return nil, nil, nil, err
				}
				var out map[string]any
				err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/"+e+"/"+op, http.MethodPost, map[string]any{"id": in.ID, "expected_updated_at": in.ExpectedUpdatedAt, "confirm_lifecycle": in.ConfirmLifecycle, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmLifecycle}, &out)
				return out, []Source{{Service: "warehousecore", Entity: e, ID: fmt.Sprint(in.ID)}}, []string{"IDs, historical product relationships and audit history are retained. Active product/brand dependencies block archive; restore validates retained identity and parent."}, err
			}
			addWritePreparationTool(server, "warehouse."+e+"s.prepare_"+op, "Prepare "+e+" "+op, "Preview all retained fields, exact version, lifecycle diff and counts of active/historical product and brand references. No cascade or mutation.", func(ctx context.Context, in WarehouseMasterLifecycleInput) (any, []Source, []string, error) {
				return invoke(ctx, in, true)
			})
			addUpdateTool(server, "warehouse."+e+"s."+op, "Warehouse "+e+" "+op, "Archive or restore one manufacturer or brand with admin/archive scope, exact version and record-bound elevated confirmation. Owner commits lifecycle, before/after audit and durable replay together.", func(ctx context.Context, in WarehouseMasterLifecycleInput) (any, []Source, []string, error) {
				return invoke(ctx, in, false)
			})
		}
	}
}

func registerWarehouseMasterAuditTools(server *mcp.Server, db *store.Store) {
	for _, entity := range []string{"manufacturer", "brand"} {
		e := entity
		addTool(server, "warehouse."+e+"s.audit_history", "Read "+e+" history", "Read redacted per-record create, update and lifecycle metadata. Raw JSON, website, IP and user-agent are excluded. Warehouse administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
			if err := requireWarehouseMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			rows, err := db.Query(ctx, `SELECT id AS audit_id,timestamp,action,user_id,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,old_values->>'name' AS name_before,COALESCE(new_values#>>'{after,name}',new_values->>'name') AS name_after,old_values->>'lifecycle_status' AS lifecycle_before,new_values#>>'{after,lifecycle_status}' AS lifecycle_after,old_values->>'manufacturer_id' AS manufacturer_before,COALESCE(new_values#>>'{after,manufacturer_id}',new_values->>'manufacturer_id') AS manufacturer_after FROM audit_log WHERE entity_type=$1 AND entity_id=$2 ORDER BY id DESC LIMIT 100`, e, in.ID)
			return rows, []Source{{Service: "warehousecore", Entity: e, ID: in.ID}}, nil, err
		})
	}
}

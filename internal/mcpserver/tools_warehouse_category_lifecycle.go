package mcpserver

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCategoryLifecycleInput struct {
	MutationControl
	ID                   string `json:"id" jsonschema:"Exact existing ID: canonical positive integer string for categories; retained string ID for subcategories and third_categories."`
	ExpectedUpdatedAt    string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond record version from the final owner preview."`
	ExpectedDependencies string `json:"expected_dependencies,omitempty" jsonschema:"Exact fingerprint from the final preview; includes current fields, descendant products and categories, and parent ancestry versions."`
	ConfirmLifecycle     bool   `json:"confirm_lifecycle,omitempty" jsonschema:"Set only after presenting retained fields, lifecycle diff and active/historical dependency counts and receiving explicit confirmation."`
	ConfirmationText     string `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound ARCHIVE or RESTORE phrase returned by the owner preview."`
}

func registerWarehouseCategoryLifecycleTools(server *mcp.Server, cfg config.Config) {
	api := newCoreAPIClient(cfg)
	for namespace, kind := range map[string]string{"categories": "category", "subcategories": "subcategory", "third_categories": "third_category"} {
		for _, op := range []string{"archive", "restore"} {
			ns, e, operation := namespace, kind, op
			invoke := func(ctx context.Context, in WarehouseCategoryLifecycleInput, preview bool) (any, []Source, []string, error) {
				if err := requireWarehouseMasterAdmin(ctx); err != nil {
					return nil, nil, nil, err
				}
				var out map[string]any
				err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/"+e+"/"+operation, http.MethodPost, map[string]any{"id": in.ID, "expected_updated_at": in.ExpectedUpdatedAt, "expected_dependencies": in.ExpectedDependencies, "confirm_lifecycle": in.ConfirmLifecycle, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmLifecycle}, &out)
				return out, []Source{{Service: "warehousecore", Entity: e, ID: in.ID}}, []string{"Retained IDs, abbreviations, hierarchy and product history are preserved. Active descendant products or categories block archive. Restore the parent ancestry first. Mutation, audit and durable receipt commit together."}, err
			}
			addWritePreparationTool(server, "warehouse."+ns+".prepare_"+operation, "Prepare category "+operation, "Preview all retained fields, exact version, lifecycle diff, parent ancestry and active/historical dependencies. No mutation or cascade.", func(ctx context.Context, in WarehouseCategoryLifecycleInput) (any, []Source, []string, error) {
				return invoke(ctx, in, true)
			})
			addUpdateTool(server, "warehouse."+ns+"."+operation, "Category "+operation, "Archive or restore one category at the named hierarchy level with actual warehouse administrator, archive scope, exact record/dependency versions and elevated record-bound confirmation.", func(ctx context.Context, in WarehouseCategoryLifecycleInput) (any, []Source, []string, error) {
				return invoke(ctx, in, false)
			})
		}
	}
}

func registerWarehouseCategoryAuditTools(server *mcp.Server, db *store.Store) {
	for namespace, kind := range map[string]string{"categories": "category", "subcategories": "subcategory", "third_categories": "third_category"} {
		ns, e := namespace, kind
		addTool(server, "warehouse."+ns+".audit_history", "Read category history", "Read up to 100 redacted per-record create/update/delete/archive/restore metadata events. Raw JSON, user agent and private data are excluded. Actual warehouse administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
			if err := requireWarehouseMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			rows, err := db.Query(ctx, `SELECT id AS audit_id,timestamp,action,user_id,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,old_values->>'name' AS name_before,COALESCE(new_values#>>'{after,name}',new_values->>'name') AS name_after,old_values->>'abbreviation' AS abbreviation_before,COALESCE(new_values#>>'{after,abbreviation}',new_values->>'abbreviation') AS abbreviation_after,old_values->>'lifecycle_status' AS lifecycle_before,new_values#>>'{after,lifecycle_status}' AS lifecycle_after,COALESCE(old_values->>'parent_id',old_values->>'category_id',old_values->>'subcategory_id') AS parent_before,COALESCE(new_values#>>'{after,parent_id}',new_values#>>'{after,category_id}',new_values#>>'{after,subcategory_id}',new_values->>'category_id',new_values->>'subcategory_id') AS parent_after FROM audit_log WHERE entity_type=$1 AND entity_id=$2 ORDER BY id DESC LIMIT 100`, e, in.ID)
			return rows, []Source{{Service: "warehousecore", Entity: e, ID: in.ID}}, nil, err
		})
	}
}

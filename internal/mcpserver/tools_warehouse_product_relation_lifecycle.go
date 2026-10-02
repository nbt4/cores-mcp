package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseRelationInput struct {
	MutationControl
	RelationID               int64    `json:"relation_id,omitempty" jsonschema:"Exact existing relationship ID for update, archive and restore; immutable product endpoints."`
	ProductID                int64    `json:"product_id,omitempty" jsonschema:"Exact source product ID for create; optional consistency check for existing actions."`
	DependencyProductID      int64    `json:"dependency_product_id,omitempty" jsonschema:"Exact distinct related product ID for create; optional consistency check for existing actions."`
	RelationType             *string  `json:"relation_type,omitempty" jsonschema:"required, recommended, compatible, consumes, alternative or included; create defaults to recommended, update preserves omission."`
	AssignmentScope          *string  `json:"assignment_scope,omitempty" jsonschema:"product, device or case; create defaults to product, update preserves omission."`
	DefaultQuantity          *float64 `json:"default_quantity,omitempty" jsonschema:"Positive quantity at most 99999999.99 with up to two decimals; create defaults to one, update preserves omission."`
	Notes                    *string  `json:"notes,omitempty" jsonschema:"Up to 500 characters; omitted preserves, explicit empty clears to null."`
	ExpectedUpdatedAt        string   `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond relationship version from owner preview; required on confirmed existing actions."`
	ExpectedProductUpdatedAt string   `json:"expected_product_updated_at,omitempty" jsonschema:"Optional exact source product version; the mandatory expected_context also binds both product versions."`
	ExpectedContext          string   `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 context from final preview, binding the complete draft, both product versions, active job references and dependency graph."`
	ConfirmChange            bool     `json:"confirm_change,omitempty" jsonschema:"Set only after presenting every field, diff, dependent jobs and business effects and obtaining explicit confirmation."`
	ConfirmationText         string   `json:"confirmation_text,omitempty" jsonschema:"Exact record-bound CREATE/UPDATE/ARCHIVE/RESTORE phrase from preview."`
}

func invokeWarehouseRelation(ctx context.Context, cfg config.Config, op string, in WarehouseRelationInput, preview bool) (any, []Source, []string, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	var out map[string]any
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/product-relations/"+op, http.MethodPost, map[string]any{"relation_id": in.RelationID, "product_id": in.ProductID, "dependency_product_id": in.DependencyProductID, "relation_type": in.RelationType, "assignment_scope": in.AssignmentScope, "default_quantity": in.DefaultQuantity, "notes": in.Notes, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "expected_product_updated_at": in.ExpectedProductUpdatedAt, "confirm_change": in.ConfirmChange, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmChange}, &out)
	relationID, sourceID, targetID := "", fmt.Sprint(in.ProductID), fmt.Sprint(in.DependencyProductID)
	for _, key := range []string{"relationship", "current", "draft"} {
		if record, ok := out[key].(map[string]any); ok {
			if v := record["relation_id"]; v != nil {
				relationID = fmt.Sprint(v)
			}
			if v := record["product_id"]; v != nil {
				sourceID = fmt.Sprint(v)
			}
			if v := record["dependency_product_id"]; v != nil {
				targetID = fmt.Sprint(v)
			}
			break
		}
	}
	return out, []Source{{Service: "warehousecore", Entity: "product_relation", ID: relationID}, {Service: "warehousecore", Entity: "product", ID: sourceID}, {Service: "warehousecore", Entity: "product", ID: targetID}}, []string{"IDs and typed relationship history are retained. Active jobs using the source or its ancestors block changes. Relationships alter future suggestions and packing expansion, and never move stock or rewrite job requirements. Owner commits relation, both product versions, audit and durable receipt together."}, err

}
func registerWarehouseRelationTools(server *mcp.Server, cfg config.Config) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		operation := op
		addWritePreparationTool(server, "warehouse.product_relations.prepare_"+operation, "Prepare product relation "+operation, "Preview all typed fields, both product versions, immutable endpoints, graph, dependent jobs and exact confirmation. No mutation.", func(ctx context.Context, in WarehouseRelationInput) (any, []Source, []string, error) {
			return invokeWarehouseRelation(ctx, cfg, operation, in, true)
		})
		execute := func(ctx context.Context, in WarehouseRelationInput) (any, []Source, []string, error) {
			return invokeWarehouseRelation(ctx, cfg, operation, in, false)
		}
		if operation == "create" {
			addCreateTool(server, "warehouse.product_relations.create", "Create product relation", "Create one typed relation with actual administrator/create scope, exact owner context, elevated confirmation and atomic audit/replay.", execute)
		} else {
			addUpdateTool(server, "warehouse.product_relations."+operation, "Product relation "+operation, "Execute the named owner workflow with actual admin/action rights, full preview/context, exact version, elevated confirmation and atomic audit/replay. Archive retains fields; restore requires active products.", execute)
		}

	}
}
func registerWarehouseRelationReads(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.product_relations.search", "Search product relations", "Read bounded typed relationship metadata, including lifecycle and exact versions. Archived records remain discoverable for restore.", func(ctx context.Context, in SearchInput) (any, []Source, []string, error) {
		if err := requireWarehouseMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT r.id AS id,r.id AS relation_id,r.product_id,p.name AS product,r.dependency_product_id,d.name AS related_product,r.relation_type,r.assignment_scope,r.default_quantity,r.is_optional,r.lifecycle_status,r.archived_at,to_char(r.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM product_dependencies r JOIN products p ON p.productid=r.product_id JOIN products d ON d.productid=r.dependency_product_id WHERE $1='' OR r.id::text=$1 OR r.product_id::text=$1 OR r.dependency_product_id::text=$1 OR p.name ILIKE $2 OR d.name ILIKE $2 OR r.relation_type ILIKE $2 ORDER BY r.id DESC LIMIT $3 OFFSET $4`, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, sourcesFor("warehousecore", "product_relation", rows), nil, err
	})
	addTool(server, "warehouse.product_relations.get", "Get product relation", "Read all typed fields and exact relationship/product versions for one ID. Warehouse administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireWarehouseMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT r.id AS relation_id,r.product_id,p.name AS product,r.dependency_product_id,d.name AS related_product,r.relation_type,r.assignment_scope,r.default_quantity,r.notes,r.is_optional,r.lifecycle_status,r.archived_at,r.created_at,to_char(r.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(p.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS product_version,to_char(d.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS related_product_version FROM product_dependencies r JOIN products p ON p.productid=r.product_id JOIN products d ON d.productid=r.dependency_product_id WHERE r.id::text=$1`, in.ID)
		return rows, []Source{{Service: "warehousecore", Entity: "product_relation", ID: in.ID}}, nil, err
	})
	addTool(server, "warehouse.product_relations.audit_history", "Read product relation history", "Read redacted per-record MCP/AI action, actor, versions, typed field and lifecycle changes. Notes and raw audit payloads are excluded. Warehouse administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireWarehouseMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,timestamp,action,user_id,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,old_values->>'relation_type' AS relation_before,COALESCE(new_values#>>'{after,relation_type}',new_values->>'relation_type') AS relation_after,old_values->>'assignment_scope' AS scope_before,COALESCE(new_values#>>'{after,assignment_scope}',new_values->>'assignment_scope') AS scope_after,old_values->>'default_quantity' AS quantity_before,COALESCE(new_values#>>'{after,default_quantity}',new_values->>'default_quantity') AS quantity_after,old_values->>'lifecycle_status' AS lifecycle_before,new_values#>>'{after,lifecycle_status}' AS lifecycle_after FROM audit_log WHERE (entity_type='product_relation' AND entity_id=$1) OR (entity_type='product' AND action='product.relation.link' AND new_values->>'relation_id'=$1) ORDER BY id DESC LIMIT 100`, in.ID)
		return rows, []Source{{Service: "warehousecore", Entity: "product_relation", ID: in.ID}}, nil, err
	})
}

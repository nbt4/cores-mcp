package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type ProcurementWorkflowLifecycleInput struct {
	MutationControl
	ID                int64  `json:"id" jsonschema:"Exact retained ProcurementCore requisition or purchase-order ID."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond record version from the final owner preview."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact owner fingerprint of all retained fields, lines, parents, receipts and related workflow state."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record/context-bound lifecycle phrase from the final preview."`
	ConfirmChange     bool   `json:"confirm_change,omitempty" jsonschema:"Set only after reviewing retained fields, blockers, original status and exact lifecycle confirmation."`
}

func isProcurementWorkflowLifecycleTool(name string) bool {
	return (strings.HasPrefix(name, "procurement.orders.") || strings.HasPrefix(name, "procurement.requisitions.")) && (strings.HasSuffix(name, ".archive") || strings.HasSuffix(name, ".restore"))
}

func invokeProcurementWorkflowLifecycle(ctx context.Context, cfg config.Config, entity, operation string, input ProcurementWorkflowLifecycleInput, preview bool) (any, []Source, []string, error) {
	if entity == "orders" {
		if err := requireProcurementAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
	}
	kind := map[string]string{"orders": "purchase_order", "requisitions": "requisition"}[entity]
	sources := []Source{{Service: "procurementcore", Entity: kind, ID: fmt.Sprint(input.ID)}}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, sources, nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, sources, nil, err
	}
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || input.DryRun || !input.ConfirmChange
	result := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/workflows/"+entity+"/"+operation, http.MethodPost, body, &result)
	if dependencies, ok := result["dependencies"].(map[string]any); ok {
		for _, spec := range []struct{ key, entity, id, service string }{{"orders", "purchase_order", "id", "procurementcore"}, {"products", "product", "id", "procurementcore"}, {"supplier", "supplier", "id", "procurementcore"}, {"suppliers", "supplier", "id", "procurementcore"}, {"requisition", "requisition", "id", "procurementcore"}, {"receipts", "goods_receipt", "id", "procurementcore"}, {"putaway_tasks", "warehouse_task", "task_id", "warehousecore"}} {
			if rows, ok := dependencies[spec.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok {
						sources = append(sources, Source{Service: spec.service, Entity: spec.entity, ID: fmt.Sprint(row[spec.id])})
					}
				}
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Workflow lifecycle retains original status, all fields, line IDs and receipt/history links. Open related orders or receipt putaway tasks block archives; restore validates original active parents. Current owner rights, archive scope, exact record/context and elevated confirmation are required. Native audit/activity and durable receipt commit atomically; replay rechecks current rights. No inventory or external-message effect."), err
}

func registerProcurementWorkflowLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	for _, namespace := range []string{"orders", "requisitions"} {
		ns := namespace
		if cfg.EnableWrites {
			for _, operation := range []string{"archive", "restore"} {
				op := operation
				addWritePreparationTool(server, "procurement."+ns+".prepare_"+op, "Prepare procurement "+ns+" "+op, "Review retained complete record/lines, related orders or putaway tasks, active parents, exact versions and bound lifecycle phrase without mutation.", func(ctx context.Context, input ProcurementWorkflowLifecycleInput) (any, []Source, []string, error) {
					return invokeProcurementWorkflowLifecycle(ctx, cfg, ns, op, input, true)
				})
				addUpdateTool(server, "procurement."+ns+"."+op, "Procurement "+ns+" "+op, "Execute the closed retained owner lifecycle after exact final context and explicit confirmation, with current archive/action rights and atomic audit/activity/durable replay.", func(ctx context.Context, input ProcurementWorkflowLifecycleInput) (any, []Source, []string, error) {
					return invokeProcurementWorkflowLifecycle(ctx, cfg, ns, op, input, false)
				})
			}
		}
		addTool(server, "procurement."+ns+".audit_history", "Read procurement "+ns+" history", "Return bounded redacted actor/action/time/version and business changes for this exact requisition or order. Current owner/admin rights are required and writes may be disabled.", func(ctx context.Context, input ProcurementMasterHistoryInput) (any, []Source, []string, error) {
			entity := map[string]string{"orders": "purchase_order", "requisitions": "requisition"}[ns]
			return procurementAuditHistory(ctx, db, ProcurementAuditInput{Entity: entity, ID: input.ID, Limit: input.Limit})
		})
	}
}

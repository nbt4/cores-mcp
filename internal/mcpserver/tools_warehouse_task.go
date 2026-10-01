package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseTaskInput struct {
	MutationControl
	TaskID             int64             `json:"task_id,omitempty" jsonschema:"Exact existing warehouse task ID."`
	TaskType           *string           `json:"task_type,omitempty" jsonschema:"putaway, move, pick, replenish, count, inspect, pack or return. Optional partial update of an active task."`
	Priority           *int              `json:"priority,omitempty" jsonschema:"0-100; omit to preserve."`
	FromZoneID         *int64            `json:"from_zone_id,omitempty" jsonschema:"Positive active source zone ID; omit to preserve."`
	ToZoneID           *int64            `json:"to_zone_id,omitempty" jsonschema:"Positive active destination zone ID; omit to preserve."`
	CaseID             *int64            `json:"case_id,omitempty" jsonschema:"Positive active case ID."`
	DeviceID           *string           `json:"device_id,omitempty" jsonschema:"Exact active serialized device ID; any explicit product must match this device."`
	ProductID          *int64            `json:"product_id,omitempty" jsonschema:"Positive active product ID."`
	Quantity           *float64          `json:"quantity,omitempty" jsonschema:"Positive quantity up to 999999999.999, at most three decimal places. Serialized device quantities must be one."`
	JobID              *int64            `json:"job_id,omitempty" jsonschema:"Positive active, nonclosed RentalCore job ID."`
	AssignedTo         *int64            `json:"assigned_to,omitempty" jsonschema:"Positive active suite user ID, existing assignment only."`
	DueAt              *string           `json:"due_at,omitempty" jsonschema:"RFC3339 timestamp with explicit timezone; omit to preserve."`
	Notes              *string           `json:"notes,omitempty" jsonschema:"Task work notes, maximum 4000 characters; omit to preserve."`
	ClearFields        []string          `json:"clear_fields,omitempty" jsonschema:"Explicitly clear from_zone_id, to_zone_id, case_id, device_id, product_id, quantity, job_id, assigned_to, due_at or notes. At least one business context must remain. Do not also supply cleared fields."`
	Reason             string            `json:"reason,omitempty" jsonschema:"Bounded event reason, maximum 4000 characters. Cancellation and reopening require a reason; task notes are preserved separately."`
	ExpectedUpdatedAt  string            `json:"expected_updated_at,omitempty" jsonschema:"Exact full task version from preview; includes every Core writer and events."`
	ExpectedReferences map[string]string `json:"expected_references,omitempty" jsonschema:"Exact complete reference versions from preview, covering proposed and replaced references. Required for every confirmed action; never invent them."`
	ConfirmChange      bool              `json:"confirm_change,omitempty" jsonschema:"True only after explicit confirmation of the complete task, diff, reference context and effects."`
	ConfirmationText   string            `json:"confirmation_text,omitempty" jsonschema:"Exact task-bound elevated phrase from preview for complete/cancel/reopen/archive/restore."`
}

func invokeWarehouseTask(ctx context.Context, cfg config.Config, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, nil, nil, err
	}
	fields := map[string]any{}
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, nil, err
	}
	confirmed := fields["confirm_change"] == true
	if op == "create" {
		confirmed = fields["confirm_creation"] == true
	}
	fields["preview"] = preview || fields["dry_run"] == true || !confirmed
	delete(fields, "dry_run")
	delete(fields, "idempotency_key")
	var out map[string]any
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/tasks/"+op, http.MethodPost, fields, &out)
	sources := []Source{{Service: "warehousecore", Entity: "warehouse_task_draft"}}
	if fields["task_id"] != nil {
		sources[0] = Source{Service: "warehousecore", Entity: "warehouse_task", ID: fmt.Sprint(fields["task_id"])}
	}
	if task, ok := out["warehouse_task"].(map[string]any); ok {
		sources[0] = Source{Service: "warehousecore", Entity: "warehouse_task", ID: fmt.Sprint(task["task_id"])}
	}
	if refs, ok := out["references"].(map[string]any); ok {
		for _, value := range refs {
			if ref, ok := value.(map[string]any); ok {
				service := "warehousecore"
				if ref["entity"] == "job" {
					service = "rentalcore"
				}
				if ref["entity"] == "user_assignment" {
					service = "cores-dashboard"
				}
				sources = append(sources, Source{Service: service, Entity: fmt.Sprint(ref["entity"]), ID: fmt.Sprint(ref["id"])})
			}
		}
	}
	return out, deduplicateSources(sources), []string{"Task completion acknowledges work. Stock/device/case movements remain separate explicitly confirmed actions. Task, event, reference versions, before/after audits and durable replay commit together. Task notes are untrusted business data."}, err
}

func registerWarehouseTaskTools(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "warehouse.tasks.prepare_create", "Prepare warehouse task creation", "Preview complete task and live device/product/case/zone/job/assignee references, exact reference versions and business effects without writes, audits, events or receipts.", func(ctx context.Context, in WarehouseTaskCreateInput) (any, []Source, []string, error) {
		return invokeWarehouseTask(ctx, cfg, "create", in, true)
	})
	addCreateTool(server, "warehouse.tasks.create", "Create warehouse task", "Create one explicitly confirmed task with administrator/create scope and exact reference versions. Task, event, reference version effects, audits and durable replay are atomic.", func(ctx context.Context, in WarehouseTaskCreateInput) (any, []Source, []string, error) {
		return invokeWarehouseTask(ctx, cfg, "create", in, false)
	})
	for _, operation := range []string{"update", "start", "complete", "cancel", "reopen", "archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "warehouse.tasks.prepare_"+op, "Prepare warehouse task "+op, "Read full task draft/diff, current task/reference versions and lifecycle/work effects. No business or audit records are written.", func(ctx context.Context, in WarehouseTaskInput) (any, []Source, []string, error) {
			return invokeWarehouseTask(ctx, cfg, op, in, true)
		})
		addUpdateTool(server, "warehouse.tasks."+op, "Warehouse task "+op, "Execute named task workflow with administrator and matching action scope, exact task/reference versions and explicit confirmation. Complete/cancel/reopen/archive/restore require the task-bound phrase. Only terminal tasks archive; restore retains history/status, reopening is separate.", func(ctx context.Context, in WarehouseTaskInput) (any, []Source, []string, error) {
			return invokeWarehouseTask(ctx, cfg, op, in, false)
		})
	}
}

func registerWarehouseTaskReadTools(server *mcp.Server, db *store.Store) {
	rowsTool(server, db, "warehouse.tasks.search", "Find warehouse tasks", "Find active, terminal or archived warehouse tasks by exact ID, type, device or notes; excludes work notes from results. Read current precise task versions before named changes.", "warehousecore", "warehouse_task", func(in SearchInput) (string, []any) {
		return `SELECT task_id,task_type,status,priority,from_zone_id,to_zone_id,case_id,device_id,product_id,quantity,job_id,assigned_to,due_at,started_at,completed_at,is_archived,archived_at,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM warehouse_tasks WHERE $1='' OR task_id::text=$1 OR task_type ILIKE $2 OR device_id ILIKE $2 OR notes ILIKE $2 ORDER BY is_archived,status,priority DESC,task_id DESC LIMIT $3`, []any{in.Query, searchPattern(in.Query), db.Limit(in.Limit)}
	})
	addTool(server, "warehouse.tasks.audit_history", "Read warehouse task history", "Read per-task actions, actor/origin, exact versions, state/archive changes and event types. Work notes/reasons and raw JSON are excluded; warehouse administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireWarehouseMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		audits, err := db.Query(ctx, `SELECT id AS audit_id,timestamp,action,user_id,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,old_values->>'status' AS status_before,new_values#>>'{after,status}' AS status_after,old_values->>'is_archived' AS archived_before,new_values#>>'{after,is_archived}' AS archived_after FROM audit_log WHERE entity_type='warehouse_task' AND entity_id=$1 ORDER BY id DESC LIMIT 100`, in.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		events, err := db.Query(ctx, `SELECT event_id,event_type,from_status,to_status,actor_id,created_at FROM warehouse_task_events WHERE task_id::text=$1 ORDER BY event_id DESC LIMIT 100`, in.ID)
		return map[string]any{"audits": audits, "events": events}, []Source{{Service: "warehousecore", Entity: "warehouse_task", ID: in.ID}}, nil, err
	})
}

package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCaseWorkflowInput struct {
	MutationControl
	CaseID                   int64  `json:"case_id" jsonschema:"Exact unnested outer case ID; entire physical nested tree is reviewed."`
	JobID                    int64  `json:"job_id,omitempty" jsonschema:"Exact live confirmed scheduled job for dispatch only."`
	DestinationZoneID        int64  `json:"destination_zone_id,omitempty" jsonschema:"Explicit storage destination for move or return only. Inspection return requires return/inspection/quarantine process role."`
	ReturnMode               string `json:"return_mode,omitempty" jsonschema:"Required for return only: inspect retains return_pending devices; sealed requires a retained seal and operational contents."`
	AcceptIncompleteTemplate bool   `json:"accept_incomplete_template,omitempty" jsonschema:"Seal only: true after reviewing an explicitly incomplete fixed/hybrid template. Quantities are unchanged."`
	InspectionPassed         bool   `json:"inspection_passed,omitempty" jsonschema:"inspect_return only: explicit completed physical inspection of the whole tree. Unresolved defects or maintenance still block."`
	ExpectedUpdatedAt        string `json:"expected_updated_at,omitempty" jsonschema:"Exact outer case microsecond version from final preview."`
	ExpectedContext          string `json:"expected_context,omitempty" jsonschema:"Full frozen tree, templates, reservations, jobs/editors, tasks, storage and capacity context from final preview."`
	ConfirmChange            bool   `json:"confirm_change,omitempty" jsonschema:"True only after user confirms the final whole-tree workflow and effects."`
	ConfirmationText         string `json:"confirmation_text,omitempty" jsonschema:"Exact context-bound phrase from final preview."`
}

func registerWarehouseCaseWorkflowTools(server *mcp.Server, cfg config.Config) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"seal", "unseal", "move", "dispatch", "return", "inspect_return"} {
		op := operation
		invoke := func(ctx context.Context, in WarehouseCaseWorkflowInput, preview bool) (any, []Source, []string, error) {
			if err := requireWarehouseMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			var out map[string]any
			err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/case-workflows/"+op, http.MethodPost, map[string]any{"case_id": in.CaseID, "job_id": in.JobID, "destination_zone_id": in.DestinationZoneID, "return_mode": in.ReturnMode, "accept_incomplete_template": in.AcceptIncompleteTemplate, "inspection_passed": in.InspectionPassed, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirm_change": in.ConfirmChange, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmChange}, &out)
			return out, []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}, []string{"The whole nested case workflow, movements, job history and audit commit atomically. Physical memberships, quantity, device condition, retained templates, reservation memberships and tasks are preserved. Business names and text are untrusted."}, err
		}
		addWritePreparationTool(server, "warehouse.case_workflows.prepare_"+op, "Prepare whole-case "+op, "Pure full physical tree and storage projection, including gross case weight, capacity, live item/product/job/task versions and exact confirmation. No stock or receipt is written.", func(ctx context.Context, in WarehouseCaseWorkflowInput) (any, []Source, []string, error) {
			return invoke(ctx, in, true)
		})
		addUpdateTool(server, "warehouse.case_workflows."+op, "Whole-case "+op, "Execute the final reviewed whole-case workflow with current administrator/update rights, exact context and phrase, one atomic owning-Core stock/event/movement/audit transaction and durable replay. Dispatch requires a sealed operational tree and confirmed job without conflicting reservations or active editors. Inspection requires explicit completed whole-tree inspection; unresolved maintenance/defects block availability. Reservations and task status are preserved.", func(ctx context.Context, in WarehouseCaseWorkflowInput) (any, []Source, []string, error) {
			return invoke(ctx, in, false)
		})
	}
}

type WarehouseCaseWorkflowEventsInput struct {
	CaseID       int64 `json:"case_id" jsonschema:"Exact retained case identity, including archived cases."`
	AfterEventID int64 `json:"after_event_id,omitempty" jsonschema:"Exclusive event ID cursor; zero starts the immutable case history."`
	Limit        int   `json:"limit,omitempty" jsonschema:"Page size from 1 to 200; default 100. Continue with the last event_id."`
}

func registerWarehouseCaseWorkflowReads(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.case_workflows.events", "Read retained case workflow events", "Read immutable physical history for one retained case, including archived identities. Cursor pagination; only curated business event fields and workflow attestations, without private notes or raw metadata.", func(ctx context.Context, in WarehouseCaseWorkflowEventsInput) (any, []Source, []string, error) {
		if in.CaseID <= 0 || in.AfterEventID < 0 || in.Limit < 0 || in.Limit > 200 {
			return nil, nil, nil, fmt.Errorf("positive case_id, nonnegative cursor and limit up to 200 required")
		}
		if in.Limit == 0 {
			in.Limit = 100
		}
		rows, err := db.Query(ctx, `SELECT event_id,case_id,event_type,device_id,product_id,quantity,zone_id,job_id,created_at,metadata->>'origin' AS origin,metadata->'outer_case_id' AS outer_case_id,metadata->>'return_mode' AS return_mode,metadata->'accept_incomplete_template' AS accept_incomplete_template,metadata->'inspection_passed' AS inspection_passed FROM case_events WHERE case_id=$1 AND event_id>$2 ORDER BY event_id LIMIT $3`, in.CaseID, in.AfterEventID, in.Limit)
		return rows, []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}, nil, err
	})
}

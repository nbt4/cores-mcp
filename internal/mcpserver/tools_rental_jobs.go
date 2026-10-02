package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RentalJobPreviewControl struct {
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond job version from final owner preview; required for existing records."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 binding complete job fields, commercial totals, related records, device conflicts and active editors."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact job/draft-bound phrase returned by final owner preview; required for confirmed execution."`
}
type RentalJobLifecycleInput struct {
	MutationControl
	RentalJobPreviewControl
	AllowDuplicate bool  `json:"allow_duplicate,omitempty" jsonschema:"For restore only: explicitly confirm distinct identity after reviewing exact active or archived matches."`
	JobID          int64 `json:"job_id" jsonschema:"Exact existing canonical job ID, including archived jobs when restoring."`
	ConfirmChange  bool  `json:"confirm_change,omitempty" jsonschema:"Set only after showing complete fields, lifecycle effects and dependencies and receiving explicit confirmation."`
}

func requireRentalJobFinancialInput(ctx context.Context, input any) error {
	raw, _ := json.Marshal(input)
	fields := map[string]any{}
	_ = json.Unmarshal(raw, &fields)
	for _, key := range []string{"revenue", "discount", "discount_type", "multiply_by_days", "prices_include_tax"} {
		if fields[key] != nil {
			info := auth.TokenInfoFromContext(ctx)
			if info == nil || !containsString(info.Scopes, "cores:rental:financial") {
				return fmt.Errorf("Commercial job fields require the explicitly selected cores:rental:financial scope")
			}
		}
	}
	return nil
}

func invokeRentalJob(ctx context.Context, cfg config.Config, db *store.Store, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := requireRentalJobFinancialInput(ctx, input); err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{}
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, nil, nil, err
	}
	dryRun, _ := body["dry_run"].(bool)
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	confirmed, _ := body["confirm_change"].(bool)
	for _, key := range []string{"confirm_creation", "confirm_update"} {
		if body[key] == true {
			confirmed = true
		}
		delete(body, key)
	}
	body["confirm_change"] = confirmed
	body["preview"] = preview || dryRun || !confirmed
	queries := map[string]string{}
	for _, key := range []string{"job", "customer", "status", "job_category", "venue"} {
		queries[key], _ = body[key+"_query"].(string)
		delete(body, key+"_query")
	}
	// Confirmed exact IDs go straight to the durable owner receipt before any
	// current-state lookups. Query-based preparation returns the resolved IDs.
	if body["preview"] == true {
		for _, ref := range []struct{ key, query string }{
			{"job", `SELECT jobid AS id,job_code AS label,description AS context FROM jobs WHERE deleted_at IS NULL`},
			{"customer", `SELECT customerid AS id,COALESCE(NULLIF(companyname,''),NULLIF(name,''),trim(concat_ws(' ',firstname,lastname))) AS label,city AS context FROM customers WHERE NOT COALESCE(is_archived,false)`},
			{"status", `SELECT statusid AS id,status AS label,'' AS context FROM status`},
			{"job_category", `SELECT jobcategoryid AS id,name AS label,'' AS context FROM jobcategory`},
			{"venue", `SELECT id,name AS label,city AS context FROM venues WHERE NOT is_archived`},
		} {
			if strings.TrimSpace(queries[ref.key]) == "" {
				continue
			}
			id := numericID(body[ref.key+"_id"])
			record, candidates, e := resolveReference(ctx, db, ref.query, id, queries[ref.key])
			if e != nil {
				return nil, nil, nil, e
			}
			if record == nil {
				return map[string]any{"operation_status": "needs_input", "ready_to_execute": false, "required_fields": []string{ref.key + "_id"}, "candidates": candidates}, []Source{{Service: "rentalcore", Entity: ref.key}}, nil, nil
			}
			body[ref.key+"_id"] = record["id"]
		}
	} else {
		for key, query := range queries {
			if query != "" && numericID(body[key+"_id"]) == 0 {
				return nil, nil, nil, fmt.Errorf("Confirmed execution requires the resolved %s_id from final preview", key)
			}
		}
	}
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/jobs/"+op, http.MethodPost, body, &out)
	id := ""
	for _, key := range []string{"job", "current", "draft"} {
		if record, ok := out[key].(map[string]any); ok && record["job_id"] != nil {
			id = fmt.Sprint(record["job_id"])
			break
		}
	}
	return out, []Source{{Service: "rentalcore", Entity: "job", ID: id}}, []string{"Complete privileged job preview and exact record/context versions are required. Active editors and changed dependencies stop execution. Commercial fields and recalculation require selected rental financial access. Archive retains related business history; closed issued devices follow the physical return workflow. Record, native history, audit and durable receipt commit atomically. No external messages are sent."}, err
}

func registerRentalJobLifecycle(server *mcp.Server, cfg config.Config, db *store.Store) {
	for _, operation := range []string{"archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "rental.jobs.prepare_"+op, "Prepare job "+op, "Review complete retained job fields, warehouse blockers, related versions and exact confirmation without changing data.", func(ctx context.Context, in RentalJobLifecycleInput) (any, []Source, []string, error) {
			return invokeRentalJob(ctx, cfg, db, op, in, true)
		})
		addUpdateTool(server, "rental.jobs."+op, "Job "+op, "Execute the named job lifecycle after exact record/context versions and explicit record-bound confirmation. Never delete business history.", func(ctx context.Context, in RentalJobLifecycleInput) (any, []Source, []string, error) {
			return invokeRentalJob(ctx, cfg, db, op, in, false)
		})
	}
}
func registerRentalJobAudit(server *mcp.Server, db *store.Store) {
	addTool(server, "rental.jobs.audit_history", "Read job mutation history", "Read bounded per-job action, actor, timestamp, result version and lifecycle/status changes. Raw audit contents and external sync IDs are excluded. Rental administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,new_values->>'origin' AS origin,new_values->>'updated_at' AS result_version,old_values->>'is_archived' AS archived_before,new_values#>>'{after,is_archived}' AS archived_after,old_values->>'status_id' AS status_before,new_values#>>'{after,status_id}' AS status_after FROM audit_log WHERE entity_type='rental_job' AND entity_id=$1 ORDER BY id DESC LIMIT 100`, strings.TrimSpace(in.ID))
		return rows, []Source{{Service: "rentalcore", Entity: "job", ID: in.ID}}, nil, err
	})
}

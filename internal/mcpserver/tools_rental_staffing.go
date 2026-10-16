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

type RentalStaffingInput struct {
	MutationControl
	JobID                int64   `json:"job_id" jsonschema:"Exact existing Rental job ID. Assignment follows the job dates; no separate employee shift times."`
	EmployeeID           int64   `json:"employee_id" jsonschema:"Exact employee ID from rental.employees.search; never a suite user ID. Identity is immutable."`
	Role                 *string `json:"role,omitempty" jsonschema:"Optional role up to100 characters for create/update. Omit preserves; empty clears. Update requires this field. Archive/restore retain role."`
	AllowOverlap         bool    `json:"allow_overlap,omitempty" jsonschema:"Only for create/restore, after explicitly reviewing schedule_conflicts in final preview; false by default. It never hides conflicts."`
	ExpectedUpdatedAt    string  `json:"expected_updated_at,omitempty" jsonschema:"Exact assignment version from final preview; required for update/archive/restore, omitted on create."`
	ExpectedJobUpdatedAt string  `json:"expected_job_updated_at,omitempty" jsonschema:"Exact parent job version from final preview, required for all executions."`
	ExpectedContext      string  `json:"expected_context,omitempty" jsonschema:"Exact final preview SHA-256 binding job, assignment, employee/skills, schedule conflicts, active editors, draft and effects."`
	ConfirmationText     string  `json:"confirmation_text,omitempty" jsonschema:"Exact record/draft-bound required_confirmation_text from final preview."`
	ConfirmChange        bool    `json:"confirm_change,omitempty" jsonschema:"Only after explicit confirmation of the final draft, conflicts and optional configured calendar synchronization."`
}
type RentalStaffingReadInput struct {
	JobID      int64 `json:"job_id"`
	EmployeeID int64 `json:"employee_id"`
}
type RentalStaffingSearchInput struct {
	JobID           int64 `json:"job_id"`
	IncludeArchived bool  `json:"include_archived,omitempty"`
	Limit           int   `json:"limit,omitempty"`
	Offset          int   `json:"offset,omitempty"`
}
type RentalEmployeeSearchInput struct {
	SearchInput
	IncludeInactive bool `json:"include_inactive,omitempty" jsonschema:"Include inactive employee identities for retained history; inactive employees cannot be newly assigned or restored."`
}

func isRentalStaffingMutation(name string) bool {
	return strings.HasPrefix(name, "rental.staffing.") && isMutationTool(name)
}
func invokeRentalStaffing(ctx context.Context, cfg config.Config, op string, in RentalStaffingInput, preview bool) (any, []Source, []string, error) {
	if e := requireRentalMasterAdmin(ctx); e != nil {
		return nil, nil, nil, e
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, nil, nil, e
	}
	body := map[string]any{}
	if e = json.Unmarshal(raw, &body); e != nil {
		return nil, nil, nil, e
	}
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || in.DryRun || !in.ConfirmChange
	out := map[string]any{}
	e = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/staffing/"+op, http.MethodPost, body, &out)
	return out, []Source{{Service: "rentalcore", Entity: "job_employee", ID: fmt.Sprintf("%d/%d", in.JobID, in.EmployeeID)}}, []string{"Review exact job/assignment versions, employee identity/skills, role diff, schedule conflicts and configured calendar effects before explicit confirmation. Reusing an employee in overlapping open jobs requires allow_overlap=true. Archive retains identity and role; restore requires an active employee and job. Native history, audit and durable replay are atomic with the assignment. Configured calendar synchronization runs best effort after commit; replay never resends it. Private employee/contact/bank fields are excluded. No financial scope is required."}, e
}

const rentalStaffingReadColumns = `je.job_id,je.employee_id,trim(concat_ws(' ',e.first_name,e.last_name)) AS employee,e.is_active AS employee_is_active,je.role,je.deleted_at IS NOT NULL AS is_archived,to_char(je.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(j.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS job_updated_at,j.startdate AS start_date,j.enddate AS end_date`

func validStaffingIDs(job, employee int64) bool {
	return job > 0 && job <= 2147483647 && employee > 0 && employee <= 2147483647
}
func registerRentalStaffing(server *mcp.Server, cfg config.Config, db *store.Store) {
	if cfg.EnableWrites {
		for _, operation := range []string{"create", "update", "archive", "restore"} {
			op := operation
			addWritePreparationTool(server, "rental.staffing.prepare_"+op, "Prepare staffing "+op, "Preview exact employee/job identity, role, scheduling conflicts, versions and calendar effects without changing data.", func(ctx context.Context, in RentalStaffingInput) (any, []Source, []string, error) {
				return invokeRentalStaffing(ctx, cfg, op, in, true)
			})
			addUpdateTool(server, "rental.staffing."+op, "Staffing "+op, "Apply the explicitly confirmed staffing operation through RentalCore with current administrator/action rights, exact final context and atomic audit/history/durable replay.", func(ctx context.Context, in RentalStaffingInput) (any, []Source, []string, error) {
				return invokeRentalStaffing(ctx, cfg, op, in, false)
			})
		}
	}
	addTool(server, "rental.employees.search", "Find assignable employee identities", "Find employee IDs, names, active state and skills for staffing. Current administrator required; private addresses, contacts, bank, birth dates and notes excluded.", func(ctx context.Context, in RentalEmployeeSearchInput) (any, []Source, []string, error) {
		if e := requireRentalMasterAdmin(ctx); e != nil {
			return nil, nil, nil, e
		}
		rows, e := db.Query(ctx, `SELECT e.id AS employee_id,trim(concat_ws(' ',e.first_name,e.last_name)) AS name,e.is_active,COALESCE((SELECT jsonb_agg(jsonb_build_object('skill_id',sk.id,'name',sk.name,'category',sk.category) ORDER BY sk.id) FROM employee_skills es JOIN skills sk ON sk.id=es.skill_id WHERE es.employee_id=e.id),'[]'::jsonb) AS skills FROM employees e WHERE `+rentalExternalCatalogReadAccess+` AND ($1 OR e.is_active) AND ($2='' OR concat_ws(' ',e.first_name,e.last_name) ILIKE $3 OR EXISTS(SELECT 1 FROM employee_skills es JOIN skills sk ON sk.id=es.skill_id WHERE es.employee_id=e.id AND sk.name ILIKE $3)) ORDER BY e.last_name,e.first_name,e.id LIMIT $4 OFFSET $5`, in.IncludeInactive, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, []Source{{Service: "rentalcore", Entity: "employee"}}, untrustedTextWarning(), e
	})
	addTool(server, "rental.skills.search", "Find staffing qualifications", "Find qualification IDs, names and categories. Administrator required; employee personal fields and freeform qualification descriptions excluded.", func(ctx context.Context, in SearchInput) (any, []Source, []string, error) {
		if e := requireRentalMasterAdmin(ctx); e != nil {
			return nil, nil, nil, e
		}
		rows, e := db.Query(ctx, `SELECT id AS skill_id,name,category FROM skills WHERE `+rentalExternalCatalogReadAccess+` AND ($1='' OR name ILIKE $2 OR category ILIKE $2) ORDER BY name,id LIMIT $3 OFFSET $4`, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, []Source{{Service: "rentalcore", Entity: "skill"}}, untrustedTextWarning(), e
	})
	addTool(server, "rental.staffing.get", "Get retained staffing assignment", "Read exact active or archived job/employee assignment and versions without private or financial fields. Current administrator required.", func(ctx context.Context, in RentalStaffingReadInput) (any, []Source, []string, error) {
		if e := requireRentalMasterAdmin(ctx); e != nil {
			return nil, nil, nil, e
		}
		if !validStaffingIDs(in.JobID, in.EmployeeID) {
			return nil, nil, nil, fmt.Errorf("Exact bounded job/employee IDs required")
		}
		rows, e := db.Query(ctx, `SELECT `+rentalStaffingReadColumns+` FROM job_employees je JOIN jobs j ON j.jobid=je.job_id JOIN employees e ON e.id=je.employee_id WHERE je.job_id=$1 AND je.employee_id=$2 AND `+rentalExternalCatalogReadAccess, in.JobID, in.EmployeeID)
		return rows, []Source{{Service: "rentalcore", Entity: "job_employee", ID: fmt.Sprintf("%d/%d", in.JobID, in.EmployeeID)}}, untrustedTextWarning(), e
	})
	addTool(server, "rental.staffing.search", "Search job staffing assignments", "Read bounded staffing for one job. include_archived discovers retained assignments for restore. Current administrator required.", func(ctx context.Context, in RentalStaffingSearchInput) (any, []Source, []string, error) {
		if e := requireRentalMasterAdmin(ctx); e != nil {
			return nil, nil, nil, e
		}
		if !validStaffingIDs(in.JobID, 1) {
			return nil, nil, nil, fmt.Errorf("Exact bounded job ID required")
		}
		rows, e := db.Query(ctx, `SELECT `+rentalStaffingReadColumns+` FROM job_employees je JOIN jobs j ON j.jobid=je.job_id JOIN employees e ON e.id=je.employee_id WHERE je.job_id=$1 AND ($2 OR je.deleted_at IS NULL) AND `+rentalExternalCatalogReadAccess+` ORDER BY je.employee_id LIMIT $3 OFFSET $4`, in.JobID, in.IncludeArchived, db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, []Source{{Service: "rentalcore", Entity: "job", ID: fmt.Sprint(in.JobID)}}, untrustedTextWarning(), e
	})
	addTool(server, "rental.staffing.audit_history", "Get staffing change history", "Read bounded native/MCP actor, timestamp, role changes and archive/result versions. No raw JSON, private employee/contact or financial fields. Current administrator required.", func(ctx context.Context, in RentalStaffingReadInput) (any, []Source, []string, error) {
		if e := requireRentalMasterAdmin(ctx); e != nil {
			return nil, nil, nil, e
		}
		if !validStaffingIDs(in.JobID, in.EmployeeID) {
			return nil, nil, nil, fmt.Errorf("Exact bounded job/employee IDs required")
		}
		id := fmt.Sprintf("%d/%d", in.JobID, in.EmployeeID)
		rows, e := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,new_values->>'origin' AS origin,old_values->>'role' AS role_before,new_values#>>'{after,role}' AS role_after,new_values#>>'{after,is_archived}' AS is_archived,new_values#>>'{after,updated_at}' AS result_version FROM audit_log WHERE entity_type='rental_staffing' AND entity_id=$1 AND `+rentalExternalCatalogReadAccess+` ORDER BY id DESC LIMIT 100`, id)
		return rows, []Source{{Service: "rentalcore", Entity: "job_employee", ID: id}}, untrustedTextWarning(), e
	})
}

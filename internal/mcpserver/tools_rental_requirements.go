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

type RentalRequirementPreviewControl struct {
	ExpectedUpdatedAt    string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond requirement version from final owner preview; required for existing records."`
	ExpectedJobUpdatedAt string `json:"expected_job_updated_at,omitempty" jsonschema:"Exact parent job version from final preview; required for every confirmed operation including create."`
	ExpectedContext      string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 binding complete quantities, parent job, product, source positions, device assignments and active editors."`
	ConfirmationText     string `json:"confirmation_text,omitempty" jsonschema:"Exact requirement/draft-bound phrase from final owner preview."`
}
type RentalRequirementLifecycleInput struct {
	MutationControl
	RentalRequirementPreviewControl
	RequirementID int64 `json:"requirement_id" jsonschema:"Exact existing requirement ID including archived records when restoring."`
	ConfirmChange bool  `json:"confirm_change,omitempty" jsonschema:"Only after showing all retained fields, source quantities, dependencies and effects and receiving explicit confirmation."`
}

func invokeRentalRequirement(ctx context.Context, cfg config.Config, db *store.Store, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
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
	dry, _ := body["dry_run"].(bool)
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	confirmed, _ := body["confirm_change"].(bool)
	for _, key := range []string{"confirm_creation", "confirm_update"} {
		confirmed = confirmed || body[key] == true
		delete(body, key)
	}
	body["confirm_change"] = confirmed
	body["preview"] = preview || dry || !confirmed
	jobQuery, _ := body["job_query"].(string)
	productQuery, _ := body["product_query"].(string)
	delete(body, "job_query")
	delete(body, "product_query")
	if body["preview"] == true {
		for _, ref := range []struct{ key, query, term string }{
			{"job", `SELECT jobid AS id,job_code AS label,description AS context FROM jobs WHERE deleted_at IS NULL`, jobQuery},
			{"product", activeProductReferenceQuery, productQuery},
		} {
			if strings.TrimSpace(ref.term) == "" {
				continue
			}
			record, candidates, e := resolveReference(ctx, db, ref.query, numericID(body[ref.key+"_id"]), ref.term)
			if e != nil {
				return nil, nil, nil, e
			}
			if record == nil {
				return map[string]any{"operation_status": "needs_input", "ready_to_execute": false, "required_fields": []string{ref.key + "_id"}, "candidates": candidates}, []Source{{Service: "rentalcore", Entity: ref.key}}, nil, nil
			}
			body[ref.key+"_id"] = record["id"]
		}
	} else {
		if jobQuery != "" && numericID(body["job_id"]) == 0 || productQuery != "" && numericID(body["product_id"]) == 0 {
			return nil, nil, nil, fmt.Errorf("Confirmed execution requires resolved job/product IDs from final preview")
		}
	}
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/requirements/"+op, http.MethodPost, body, &out)
	id := ""
	jobID := ""
	productID := ""
	for _, key := range []string{"requirement", "current", "draft"} {
		if record, ok := out[key].(map[string]any); ok {
			if record["requirement_id"] != nil {
				id = fmt.Sprint(record["requirement_id"])
			}
			if record["job_id"] != nil {
				jobID = fmt.Sprint(record["job_id"])
			}
			if record["product_id"] != nil {
				productID = fmt.Sprint(record["product_id"])
			}
			if id != "" {
				break
			}
		}
	}
	return out, []Source{{Service: "rentalcore", Entity: "job_product_requirement", ID: id}, {Service: "rentalcore", Entity: "job", ID: jobID}, {Service: "warehousecore", Entity: "product", ID: productID}}, []string{"Current administrator/action rights and exact requirement/job/context versions are required. Quantity is the total; manual_quantity controls additional material. Position contributions remain server-controlled and are included in the total. Archive preserves identity and quantities and blocks source positions or assigned equipment; archived material is excluded from live demand and packing. Restore preserves every field and validates parent/product/source context. Requirement, native job history, audit and durable receipt commit atomically. No stock movements, price changes or external messages."}, err
}

func registerRentalRequirementLifecycle(server *mcp.Server, cfg config.Config, db *store.Store) {
	for _, operation := range []string{"archive", "restore"} {
		op := operation
		addWritePreparationTool(server, "rental.requirements.prepare_"+op, "Prepare requirement "+op, "Review exact line/job versions, original quantities, source positions and equipment before lifecycle confirmation.", func(ctx context.Context, in RentalRequirementLifecycleInput) (any, []Source, []string, error) {
			return invokeRentalRequirement(ctx, cfg, db, op, in, true)
		})
		addUpdateTool(server, "rental.requirements."+op, "Requirement "+op, "Execute the named requirement lifecycle after exact owner preview and explicit record-bound confirmation; preserve business identity/history.", func(ctx context.Context, in RentalRequirementLifecycleInput) (any, []Source, []string, error) {
			return invokeRentalRequirement(ctx, cfg, db, op, in, false)
		})
	}
}

func registerRentalRequirementReads(server *mcp.Server, db *store.Store) {
	base := `SELECT r.id AS requirement_id,r.id AS id,r.job_id,j.job_code,r.product_id,p.name AS product,r.quantity,r.manual_quantity,r.position_quantity,r.created_at,r.deleted_at IS NOT NULL AS is_archived,r.deleted_at AS archived_at,to_char(r.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(j.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS job_updated_at FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id JOIN products p ON p.productid=r.product_id`
	addTool(server, "rental.requirements.get", "Read exact material requirement", "Read complete total/manual/position quantities, stable identity, lifecycle and exact requirement/job versions, including archives.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		rows, err := db.Query(ctx, base+` WHERE r.id::text=$1`, strings.TrimSpace(in.ID))
		return rows, []Source{{Service: "rentalcore", Entity: "job_product_requirement", ID: in.ID}}, nil, err
	})
	addTool(server, "rental.requirements.search", "Search material requirements", "Find bounded active or archived material lines by exact line/job/product ID, job code, product name or job title. Archives are labelled and remain excluded from live demand.", func(ctx context.Context, in SearchInput) (any, []Source, []string, error) {
		rows, err := db.Query(ctx, base+` WHERE $1='' OR r.id::text=$1 OR r.job_id::text=$1 OR r.product_id::text=$1 OR j.job_code ILIKE $2 OR p.name ILIKE $2 OR j.description ILIKE $2 ORDER BY r.id DESC LIMIT $3 OFFSET $4`, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, sourcesFor("rentalcore", "job_product_requirement", rows), nil, err
	})
	addTool(server, "rental.requirements.audit_history", "Read material requirement history", "Read bounded action, actor, timestamp, result version and lifecycle/quantity changes. Raw audits and unrelated job/private data are excluded; rental administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,new_values->>'origin' AS origin,new_values->>'updated_at' AS result_version,old_values->>'quantity' AS quantity_before,new_values#>>'{after,quantity}' AS quantity_after,old_values->>'manual_quantity' AS manual_before,new_values#>>'{after,manual_quantity}' AS manual_after,old_values->>'is_archived' AS archived_before,new_values#>>'{after,is_archived}' AS archived_after FROM audit_log WHERE entity_type='rental_requirement' AND entity_id=$1 ORDER BY id DESC LIMIT 100`, strings.TrimSpace(in.ID))
		return rows, []Source{{Service: "rentalcore", Entity: "job_product_requirement", ID: in.ID}}, nil, err
	})
}

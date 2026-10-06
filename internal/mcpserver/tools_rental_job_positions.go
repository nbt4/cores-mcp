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

// RentalJobPositionInput intentionally supports product order lines, matching + Produkt.
// Job/product identity cannot be changed on update and archive preserves every field.
type RentalJobPositionPreviewControl struct {
	ExpectedUpdatedAt    string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond position version from final owner preview; required for update/archive."`
	ExpectedJobUpdatedAt string `json:"expected_job_updated_at,omitempty" jsonschema:"Exact parent job version from final preview; required for every confirmed operation."`
	ExpectedContext      string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 of position draft, job, product, all position/material sources, device assignments, active editors and commercial effects."`
	ConfirmationText     string `json:"confirmation_text,omitempty" jsonschema:"Exact position/draft-bound phrase from the final owner preview."`
}

type RentalJobPositionInput struct {
	MutationControl
	RentalJobPositionPreviewControl
	PositionID             int64    `json:"position_id,omitempty" jsonschema:"Exact position ID for update/archive; omit for create."`
	JobID                  int64    `json:"job_id,omitempty" jsonschema:"Parent rental job ID; required for create. Immutable thereafter."`
	JobQuery               string   `json:"job_query,omitempty" jsonschema:"Resolve parent job by code or title during preparation only."`
	ProductID              int64    `json:"product_id,omitempty" jsonschema:"Existing active warehouse product ID; required for create. Immutable thereafter."`
	ProductQuery           string   `json:"product_query,omitempty" jsonschema:"Resolve warehouse product by name/code during preparation only."`
	Description            *string  `json:"description,omitempty" jsonschema:"Position description; defaults to product name on create. Empty clears."`
	Quantity               *float64 `json:"quantity,omitempty" jsonschema:"Positive whole product quantity up to 99999999; required explicitly on create."`
	Unit                   *string  `json:"unit,omitempty" jsonschema:"Nonempty unit up to 50 bytes; defaults to Stück."`
	UnitPrice              *float64 `json:"unit_price,omitempty" jsonschema:"Explicit reviewed unit price on create, zero allowed. Two decimals; no implicit zero price."`
	FollowDayFactor        *float64 `json:"follow_day_factor,omitempty" jsonschema:"Nonnegative additional day factor up to 99.99; defaults to 0.50."`
	DiscountPercent        *float64 `json:"discount_percent,omitempty" jsonschema:"Line percentage discount from 0 to 100; defaults to zero."`
	DiscountAmount         *float64 `json:"discount_amount,omitempty" jsonschema:"Nonnegative line amount discount with two decimals; defaults to zero."`
	TaxRate                *float64 `json:"tax_rate,omitempty" jsonschema:"Nonnegative tax percentage up to 999.99; defaults to 19.00."`
	ManualQuantity         *int64   `json:"manual_quantity,omitempty" jsonschema:"Optional replacement of existing additional manual material, never an increment. Set 0 to migrate manual demand to this commercial position atomically. Not allowed on archive."`
	PreserveManualQuantity bool     `json:"preserve_manual_quantity,omitempty" jsonschema:"Explicitly retain reviewed independent additional manual demand. Required if existing manual demand is positive and manual_quantity is omitted; mutually exclusive with manual_quantity."`
	AllowDuplicate         bool     `json:"allow_duplicate,omitempty" jsonschema:"Create only: acknowledge existing same-product commercial positions after reviewing duplicates."`
	ConfirmChange          bool     `json:"confirm_change,omitempty" jsonschema:"Set only after complete position, material source and job-value preview and explicit user confirmation."`
}

func requireRentalPositionFinancial(ctx context.Context) error {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || !containsString(info.Scopes, "cores:rental:financial") {
		return fmt.Errorf("Order positions and their job-value effects require the separately selected cores:rental:financial scope")
	}
	return nil
}
func invokeRentalJobPosition(ctx context.Context, cfg config.Config, db *store.Store, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := requireRentalPositionFinancial(ctx); err != nil {
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
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/job-positions/"+op, http.MethodPost, body, &out)
	id := ""
	jobID := ""
	productID := ""
	for _, key := range []string{"position", "current", "draft"} {
		if record, ok := out[key].(map[string]any); ok {
			if record["position_id"] != nil {
				id = fmt.Sprint(record["position_id"])
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
	return out, []Source{{Service: "rentalcore", Entity: "job_position", ID: id}, {Service: "rentalcore", Entity: "job", ID: jobID}, {Service: "warehousecore", Entity: "product", ID: productID}}, []string{"Product order positions generate material demand and recalculate job value through the native owner workflow. Existing manual quantities must be explicitly replaced or preserved; manual_quantity=0 transfers matching manual demand without doubling it. Current rental administrator/action rights, separately selected rental financial access, exact position/job/context versions, final bound confirmation and durable idempotency are required. Archive retains identity/history, excludes positions from material demand and revenue, and blocks assigned equipment. Position, demand, revenue, history, audit and receipt commit atomically. Procurement and physical stock are unchanged."}, err
}

func registerRentalJobPositionWrites(server *mcp.Server, cfg config.Config, db *store.Store) {
	for _, operation := range []string{"create", "update", "archive"} {
		op := operation
		addWritePreparationTool(server, "rental.job_positions.prepare_"+op, "Prepare job position "+op, "Review a product order position, duplicate positions, manual versus source demand, calculated job value, exact versions and bound confirmation without changing data.", func(ctx context.Context, in RentalJobPositionInput) (any, []Source, []string, error) {
			return invokeRentalJobPosition(ctx, cfg, db, op, in, true)
		})
		fn := func(ctx context.Context, in RentalJobPositionInput) (any, []Source, []string, error) {
			return invokeRentalJobPosition(ctx, cfg, db, op, in, false)
		}
		if op == "create" {
			addCreateTool(server, "rental.job_positions."+op, "Create job product position", "Create the confirmed commercial product line through RentalCore with atomic demand/revenue reconciliation. Explicitly review existing manual demand to prevent duplication.", fn)
		} else {
			addUpdateTool(server, "rental.job_positions."+op, "Job position "+op, "Execute the exact reviewed product position change or retained archive through RentalCore, with administrator/action and financial scopes, versions, bound confirmation and durable replay.", fn)
		}
	}
}
func registerRentalJobPositionReads(server *mcp.Server, db *store.Store) {
	base := `SELECT p.position_id,p.position_id AS id,p.job_id,j.job_code,p.product_id,pr.name AS product,p.position_type,p.description,p.quantity,p.unit,p.unit_price,p.follow_day_factor,p.discount_percent,p.discount_amount,p.tax_rate,p.sort_order,p.created_at,p.deleted_at IS NOT NULL AS is_archived,p.deleted_at AS archived_at,to_char(p.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(j.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS job_updated_at FROM job_positions p JOIN jobs j ON j.jobid=p.job_id LEFT JOIN products pr ON pr.productid=p.product_id`
	addTool(server, "rental.job_positions.get", "Read exact order position", "Read a commercial job position with pricing, quantities, archive state and exact versions. Separately selected rental financial access required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalPositionFinancial(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, base+` WHERE p.position_id::text=$1`, strings.TrimSpace(in.ID))
		return rows, []Source{{Service: "rentalcore", Entity: "job_position", ID: in.ID}}, nil, err
	})
	addTool(server, "rental.job_positions.search", "Search order positions", "Find active or retained archived positions by exact position/job/product ID, job code, job title, product name or description. Separately selected rental financial access required.", func(ctx context.Context, in SearchInput) (any, []Source, []string, error) {
		if err := requireRentalPositionFinancial(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, base+` WHERE $1='' OR p.position_id::text=$1 OR p.job_id::text=$1 OR p.product_id::text=$1 OR j.job_code ILIKE $2 OR j.description ILIKE $2 OR pr.name ILIKE $2 OR p.description ILIKE $2 ORDER BY p.job_id DESC,p.sort_order,p.position_id LIMIT $3 OFFSET $4`, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, sourcesFor("rentalcore", "job_position", rows), nil, err
	})
	addTool(server, "rental.job_positions.audit_history", "Read order position history", "Read bounded action, actor, time, result version and quantities/archive changes; rental administrator required. Raw audit contents and financial/private fields are excluded.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,new_values->>'origin' AS origin,new_values->>'updated_at' AS result_version,old_values->>'quantity' AS quantity_before,new_values#>>'{after,quantity}' AS quantity_after,old_values->>'is_archived' AS archived_before,new_values#>>'{after,is_archived}' AS archived_after FROM audit_log WHERE entity_type='rental_job_position' AND entity_id=$1 ORDER BY id DESC LIMIT 100`, strings.TrimSpace(in.ID))
		return rows, []Source{{Service: "rentalcore", Entity: "job_position", ID: in.ID}}, nil, err
	})
}

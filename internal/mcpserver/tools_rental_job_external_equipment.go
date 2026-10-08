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

type RentalJobExternalEquipmentInput struct {
	MutationControl
	JobID                int64  `json:"job_id,omitempty" jsonschema:"Existing active rental job ID, resolved in final preparation."`
	JobQuery             string `json:"job_query,omitempty" jsonschema:"Resolve job code/title during preparation only; ambiguities require an exact ID."`
	EquipmentID          int64  `json:"equipment_id,omitempty" jsonschema:"Existing active external rental catalog ID; never a warehouse product/device ID."`
	EquipmentQuery       string `json:"equipment_query,omitempty" jsonschema:"Resolve external rental catalog name/supplier during preparation only."`
	Quantity             *int64 `json:"quantity,omitempty" jsonschema:"Explicit whole quantity 1–1000; no inferred stock or default quantity."`
	DaysUsed             *int64 `json:"days_used,omitempty" jsonschema:"Explicit rental duration 1–365 days; the job multiply_by_days setting controls cost calculation."`
	RepairExisting       bool   `json:"repair_existing,omitempty" jsonschema:"Explicit repair: create only a missing rental position for an existing unlinked supplier assignment; preserve its quantity, days, costs and history. Blocks existing rental positions."`
	Notes                string `json:"notes,omitempty" jsonschema:"Optional assignment notes up to 500 bytes."`
	ExpectedJobUpdatedAt string `json:"expected_job_updated_at,omitempty" jsonschema:"Exact microsecond job version from final preview; required on execution."`
	ExpectedContext      string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 binding job, catalog prices, assignments, active editors and draft from final preview."`
	ConfirmationText     string `json:"confirmation_text,omitempty" jsonschema:"Exact job/equipment/draft-bound phrase from final preview."`
	ConfirmChange        bool   `json:"confirm_change,omitempty" jsonschema:"Set only after presenting full assignment and costs and receiving explicit user confirmation."`
}

func invokeRentalJobExternalEquipment(ctx context.Context, cfg config.Config, db *store.Store, in RentalJobExternalEquipmentInput, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := requireRentalPositionFinancial(ctx); err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{}
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, nil, nil, err
	}
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	delete(body, "job_query")
	delete(body, "equipment_query")
	body["preview"] = preview || in.DryRun || !in.ConfirmChange
	if body["preview"] == true {
		for _, ref := range []struct{ key, query, term string }{
			{"job", `SELECT jobid AS id,job_code AS label,description AS context FROM jobs WHERE deleted_at IS NULL`, in.JobQuery},
			{"equipment", `SELECT id,name AS label,concat_ws(' ',supplier,category) AS context FROM rental_equipment WHERE is_active=true`, in.EquipmentQuery},
		} {
			if strings.TrimSpace(ref.term) == "" {
				continue
			}
			record, candidates, e := resolveReference(ctx, db, ref.query, numericID(body[ref.key+"_id"]), ref.term)
			if e != nil {
				return nil, nil, nil, e
			}
			if record == nil {
				return map[string]any{"operation_status": "needs_input", "ready_to_execute": false, "required_fields": []string{ref.key + "_id"}, "candidates": candidates}, []Source{{Service: "rentalcore", Entity: "job"}, {Service: "warehousecore", Entity: "rental_equipment"}}, nil, nil
			}
			body[ref.key+"_id"] = record["id"]
		}
	} else if in.JobID == 0 || in.EquipmentID == 0 {
		return nil, nil, nil, fmt.Errorf("Confirmed execution requires resolved job_id and equipment_id from final preview")
	}
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/jobs/external-equipment-create", http.MethodPost, body, &out)
	jobID, equipmentID := "", ""
	for _, key := range []string{"assignment", "draft"} {
		if record, ok := out[key].(map[string]any); ok {
			if record["job_id"] != nil {
				jobID = fmt.Sprint(record["job_id"])
			}
			if record["equipment_id"] != nil {
				equipmentID = fmt.Sprint(record["equipment_id"])
			}
			break
		}
	}
	sources := []Source{{Service: "rentalcore", Entity: "job_rental_equipment", ID: jobID + "/" + equipmentID}, {Service: "rentalcore", Entity: "job", ID: jobID}, {Service: "warehousecore", Entity: "rental_equipment", ID: equipmentID}}
	if position, ok := out["position"].(map[string]any); ok && position["position_id"] != nil {
		sources = append(sources, Source{Service: "rentalcore", Entity: "job_position", ID: fmt.Sprint(position["position_id"])})
	}
	return out, sources, []string{"Creates a canonical rental job position at customer_price and a linked supplier-cost ledger at rental_price, then recalculates job revenue through RentalCore. Both prices must exist; explicit zero is allowed. Quantity and supplier rental days are explicit. Existing assignments block normal creation. repair_existing creates only a missing position and preserves existing costs/days/quantity; existing positions or linked assignments block repair. Current administrator/create rights, separately selected rental financial access, exact job/context versions and bound confirmation are required. Position, supplier costs, recalculated revenue, job version, native history, audit and durable receipt commit atomically. Procurement and physical stock are unchanged."}, err
}

func registerRentalJobExternalEquipmentWrites(server *mcp.Server, cfg config.Config, db *store.Store) {
	addWritePreparationTool(server, "rental.job_external_equipment.prepare_create", "Prepare job external rental assignment", "Resolve an existing job and external rental catalog item; preview quantity, supplier days, both catalog prices, supplier costs, net/gross sales, net margin, job revenue changes, duplicates, editors and exact confirmation without changing data.", func(ctx context.Context, in RentalJobExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalJobExternalEquipment(ctx, cfg, db, in, true)
	})
	addCreateTool(server, "rental.job_external_equipment.create", "Assign external rental equipment to job", "Create the explicitly confirmed rental position and linked costs atomically through RentalCore, or explicitly repair a missing legacy position. Refuse existing positions; require exact preview, selected create/financial scopes and durable idempotency.", func(ctx context.Context, in RentalJobExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalJobExternalEquipment(ctx, cfg, db, in, false)
	})
}

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

type RentalAssignmentControl struct {
	MutationControl
	JobID                     int64  `json:"job_id" jsonschema:"Exact existing job ID; immutable identity."`
	EquipmentID               int64  `json:"equipment_id" jsonschema:"Exact external catalog ID; immutable identity."`
	PositionID                int64  `json:"position_id" jsonschema:"Exact retained rental position ID from get/search; legacy unlinked assignments require explicit repair first."`
	ExpectedUpdatedAt         string `json:"expected_updated_at,omitempty" jsonschema:"Exact supplier ledger version from final owner preview, required on execution."`
	ExpectedPositionUpdatedAt string `json:"expected_position_updated_at,omitempty" jsonschema:"Exact linked position version from final owner preview, required on execution."`
	ExpectedJobUpdatedAt      string `json:"expected_job_updated_at,omitempty" jsonschema:"Exact parent job version from final owner preview, required on execution."`
	ExpectedContext           string `json:"expected_context,omitempty" jsonschema:"Final SHA-256 binding current full assignment, position, job, catalog, editors and commercial effects."`
	ConfirmationText          string `json:"confirmation_text,omitempty" jsonschema:"Exact record and draft bound required_confirmation_text from final preview."`
	ConfirmChange             bool   `json:"confirm_change,omitempty" jsonschema:"Set only after reviewing final diff and explicit business confirmation."`
}
type RentalAssignmentUpdateInput struct {
	RentalAssignmentControl
	Quantity        *int64   `json:"quantity,omitempty" jsonschema:"Whole quantity1–1000. Omit to preserve."`
	DaysUsed        *int64   `json:"days_used,omitempty" jsonschema:"Supplier rental days1–365. Omit to preserve; current job multiply_by_days setting controls costs."`
	RentalUnitPrice *float64 `json:"rental_unit_price,omitempty" jsonschema:"Explicit supplier unit snapshot in EUR, nonnegative with two decimals up to9999999999.99. Omit to preserve exact captured NUMERIC precision; never adopts current catalog price."`
	CustomerPrice   *float64 `json:"customer_price,omitempty" jsonschema:"Customer position unit price in EUR, nonnegative with two decimals up to9999999999.99. Omit preserves captured price."`
	Description     *string  `json:"description,omitempty" jsonschema:"Commercial position description up to10000 characters. Empty clears; omitted preserves."`
	Notes           *string  `json:"notes,omitempty" jsonschema:"Supplier assignment note up to500 bytes. Empty clears; omitted preserves."`
	Unit            *string  `json:"unit,omitempty" jsonschema:"Nonempty commercial unit up to50 characters."`
	FollowDayFactor *float64 `json:"follow_day_factor,omitempty" jsonschema:"Commercial follow-day factor0–99.99 with two decimals. Independent from supplier days_used."`
	DiscountPercent *float64 `json:"discount_percent,omitempty" jsonschema:"Line discount percent0–100 with two decimals."`
	DiscountAmount  *float64 `json:"discount_amount,omitempty" jsonschema:"Line discount amount in EUR, nonnegative with two decimals up to9999999999.99."`
	TaxRate         *float64 `json:"tax_rate,omitempty" jsonschema:"Tax rate0–100 percent with two decimals."`
	SortOrder       *int64   `json:"sort_order,omitempty" jsonschema:"Nonnegative position sort order up to2147483647."`
}
type RentalAssignmentReadInput struct {
	JobID       int64 `json:"job_id" jsonschema:"Exact rental job ID, including archived jobs for retained history."`
	EquipmentID int64 `json:"equipment_id" jsonschema:"Exact external catalog ID, never a warehouse product/device ID."`
}
type RentalAssignmentSearchInput struct {
	JobID           int64 `json:"job_id" jsonschema:"Exact rental job ID."`
	IncludeArchived bool  `json:"include_archived,omitempty" jsonschema:"Include retained archived positions for restore; active only by default."`
	Limit           int   `json:"limit,omitempty"`
	Offset          int   `json:"offset,omitempty"`
}

func isRentalAssignmentMutation(name string) bool {
	return strings.HasPrefix(name, "rental.job_external_equipment.") && isMutationTool(name)
}
func invokeRentalAssignmentChange(ctx context.Context, cfg config.Config, op string, in any, preview bool) (any, []Source, []string, error) {
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
	dry, _ := body["dry_run"].(bool)
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || dry || body["confirm_change"] != true
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/jobs/external-equipment-"+op, http.MethodPost, body, &out)
	id := fmt.Sprint(body["job_id"]) + "/" + fmt.Sprint(body["equipment_id"])
	return out, []Source{{Service: "rentalcore", Entity: "job_rental_equipment", ID: id}, {Service: "rentalcore", Entity: "job_position", ID: fmt.Sprint(body["position_id"])}}, []string{"Review exact assignment/position/job versions, full before/after fields and commercial effects. Omitted supplier/customer unit prices preserve captured snapshots; catalog changes never silently reprice existing assignments. Archive retains identity/costs and removes current revenue/cost contribution; restore retains fields, requires an active catalog and uses the current job rental-day setting. Legacy unlinked assignments require explicit existing repair first. Administrator/action and separately consented Rental financial rights are required before preview/execution/replay. Position, costs, revenue, audit, native history and durable replay commit atomically. No stock, procurement or supplier messages are changed."}, err
}

const rentalAssignmentReadColumns = `c.job_id,c.equipment_id,c.position_id,c.quantity,c.days_used,c.rental_unit_price,c.rental_unit_price::text AS rental_unit_price_exact,c.total_cost,p.unit_price AS customer_price,p.description,p.unit,p.follow_day_factor,p.discount_percent,p.discount_amount,p.tax_rate,p.sort_order,p.deleted_at IS NOT NULL AS is_archived,to_char(c.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(p.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS position_updated_at,to_char(j.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS job_updated_at,c.position_id IS NULL AS requires_legacy_repair`

func registerRentalAssignmentLifecycle(server *mcp.Server, cfg config.Config, db *store.Store) {
	if cfg.EnableWrites {
		addWritePreparationTool(server, "rental.job_external_equipment.prepare_update", "Prepare rental assignment update", "Review captured supplier/customer prices, exact quantities/days, retained commercial fields, editors and full revenue/cost changes.", func(ctx context.Context, in RentalAssignmentUpdateInput) (any, []Source, []string, error) {
			return invokeRentalAssignmentChange(ctx, cfg, "update", in, true)
		})
		addUpdateTool(server, "rental.job_external_equipment.update", "Update rental assignment", "Commit the exact confirmed position, supplier costs, job revenue and audit/receipt through RentalCore.", func(ctx context.Context, in RentalAssignmentUpdateInput) (any, []Source, []string, error) {
			return invokeRentalAssignmentChange(ctx, cfg, "update", in, false)
		})
		for _, operation := range []string{"archive", "restore"} {
			op := operation
			addWritePreparationTool(server, "rental.job_external_equipment.prepare_"+op, "Prepare rental assignment "+op, "Review retained position identity and supplier snapshots, active catalog and exact lifecycle/revenue effects.", func(ctx context.Context, in RentalAssignmentControl) (any, []Source, []string, error) {
				return invokeRentalAssignmentChange(ctx, cfg, op, in, true)
			})
			addUpdateTool(server, "rental.job_external_equipment."+op, "Rental assignment "+op, "Apply the named retained lifecycle with exact versions/context, bound confirmation, archive plus financial rights and durable atomic audit/replay.", func(ctx context.Context, in RentalAssignmentControl) (any, []Source, []string, error) {
				return invokeRentalAssignmentChange(ctx, cfg, op, in, false)
			})
		}
	}
	addTool(server, "rental.job_external_equipment.get", "Get retained rental assignment", "Read one exact active/archived supplier assignment and linked commercial position, including versions for restore. Current administrator and separate Rental financial scope required. Notes excluded.", func(ctx context.Context, in RentalAssignmentReadInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		if in.JobID < 1 || in.JobID > 2147483647 || in.EquipmentID < 1 || in.EquipmentID > 2147483647 {
			return nil, nil, nil, fmt.Errorf("Exact bounded job/equipment IDs required")
		}
		rows, err := db.Query(ctx, `SELECT `+rentalAssignmentReadColumns+` FROM job_rental_equipment c JOIN jobs j ON j.jobid=c.job_id LEFT JOIN job_positions p ON p.position_id=c.position_id WHERE c.job_id=$1 AND c.equipment_id=$2 AND `+rentalExternalCatalogReadAccess, in.JobID, in.EquipmentID)
		return rows, []Source{{Service: "rentalcore", Entity: "job_rental_equipment", ID: fmt.Sprintf("%d/%d", in.JobID, in.EquipmentID)}}, untrustedTextWarning(), err
	})
	addTool(server, "rental.job_external_equipment.search", "Search job rental assignments", "List active assignments for an exact job; include_archived discovers retained IDs/versions for restore. Current administrator and separate Rental financial scope required. Notes excluded.", func(ctx context.Context, in RentalAssignmentSearchInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		if in.JobID < 1 || in.JobID > 2147483647 {
			return nil, nil, nil, fmt.Errorf("Exact bounded job ID required")
		}
		rows, err := db.Query(ctx, `SELECT `+rentalAssignmentReadColumns+` FROM job_rental_equipment c JOIN jobs j ON j.jobid=c.job_id LEFT JOIN job_positions p ON p.position_id=c.position_id WHERE c.job_id=$1 AND ($2 OR p.deleted_at IS NULL) AND `+rentalExternalCatalogReadAccess+` ORDER BY c.equipment_id LIMIT $3 OFFSET $4`, in.JobID, in.IncludeArchived, db.Limit(in.Limit), cleanOffset(in.Offset))
		return rows, []Source{{Service: "rentalcore", Entity: "job", ID: fmt.Sprint(in.JobID)}}, untrustedTextWarning(), err
	})
	addTool(server, "rental.job_external_equipment.audit_history", "Read rental assignment history", "Bounded owner/native lifecycle history: actor, operation, timestamp, position, archive state and exact result versions. No financial values, notes or raw JSON. Current administrator required.", func(ctx context.Context, in RentalAssignmentReadInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		if in.JobID < 1 || in.JobID > 2147483647 || in.EquipmentID < 1 || in.EquipmentID > 2147483647 {
			return nil, nil, nil, fmt.Errorf("Exact bounded job/equipment IDs required")
		}
		id := fmt.Sprintf("%d/%d", in.JobID, in.EquipmentID)
		rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,COALESCE(new_values->>'origin','UI') AS origin,new_values#>>'{after,position_id}' AS position_id,new_values#>>'{after,updated_at}' AS result_version,new_values#>>'{position,updated_at}' AS position_version,COALESCE(new_values#>>'{position,is_archived}',CASE WHEN new_values#>>'{position,deleted_at}' IS NULL THEN 'false' ELSE 'true' END) AS is_archived FROM audit_log WHERE entity_type='rental_job_external_equipment' AND entity_id=$1 AND `+rentalExternalCatalogReadAccess+` ORDER BY id DESC LIMIT 100`, id)
		return rows, []Source{{Service: "rentalcore", Entity: "job_rental_equipment", ID: id}}, nil, err
	})
}

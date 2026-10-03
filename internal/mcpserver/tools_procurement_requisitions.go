package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RequisitionLineInput struct {
	LineID              int64   `json:"line_id,omitempty" jsonschema:"For update, retain this original line ID; omit to create a replacement line."`
	ProductID           *int64  `json:"product_id,omitempty" jsonschema:"Existing active product ID, or omit for a free-text line."`
	Description         string  `json:"description,omitempty"`
	Quantity            float64 `json:"quantity,omitempty"`
	Unit                string  `json:"unit,omitempty"`
	EstimatedPriceCents int64   `json:"estimated_price_cents,omitempty"`
	PreferredSupplierID *int64  `json:"preferred_supplier_id,omitempty"`
	PurchaseURL         string  `json:"purchase_url,omitempty"`
}

type RequisitionDraftInput struct {
	MutationControl
	RequisitionID     int64                   `json:"requisition_id,omitempty" jsonschema:"Existing draft ID for prepare_update/update; omit for creation."`
	Title             *string                 `json:"title,omitempty"`
	CostCenter        *string                 `json:"cost_center,omitempty"`
	Justification     *string                 `json:"justification,omitempty"`
	NeededBy          *string                 `json:"needed_by,omitempty" jsonschema:"RFC3339 date; empty string clears."`
	Lines             *[]RequisitionLineInput `json:"lines,omitempty" jsonschema:"Complete line list for create or replacement on update; omit to retain all original lines and IDs."`
	ExpectedUpdatedAt string                  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update."`
	ExpectedContext   string                  `json:"expected_context,omitempty" jsonschema:"Exact full draft/reference/duplicate context from preparation."`
	ConfirmationText  string                  `json:"confirmation_text,omitempty" jsonschema:"Exact final context-bound phrase from preparation."`
	AllowDuplicate    bool                    `json:"allow_duplicate,omitempty" jsonschema:"Create a reviewed distinct demand despite same-title own retained records."`
	ConfirmCreation   bool                    `json:"confirm_creation,omitempty"`
	ConfirmUpdate     bool                    `json:"confirm_update,omitempty"`
}

type RequisitionSubmitInput struct {
	MutationControl
	RequisitionID     int64  `json:"requisition_id,omitempty"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact full record/line/reference context from prepare_submit."`
	ConfirmSubmit     bool   `json:"confirm_submit,omitempty"`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact phrase shown by prepare_submit."`
}

func isProcurementRequisitionDraftTool(name string) bool {
	return name == "procurement.requisitions.create" || name == "procurement.requisitions.update" || name == "procurement.requisitions.submit"
}
func registerProcurementRequisitionTools(server *mcp.Server, cfg config.Config, _ *store.Store) {
	for _, op := range []string{"create", "update"} {
		prepare := func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
			return invokeProcurementRequisitionDraft(ctx, cfg, op, input, true)
		}
		execute := func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
			return invokeProcurementRequisitionDraft(ctx, cfg, op, input, false)
		}
		addWritePreparationTool(server, "procurement.requisitions.prepare_"+op, "Prepare requisition "+op, "Review the complete requester-owned draft, current catalog references, exact version/context and bound confirmation.", prepare)
		if op == "create" {
			addCreateTool(server, "procurement.requisitions.create", "Create requisition draft", "Create a complete reviewed draft with current owner rights and atomic audit/activity/durable response.", execute)
		} else {
			addUpdateTool(server, "procurement.requisitions.update", "Update requisition draft", "Update an original requester-owned draft; omitted lines retain original IDs. Exact final context and confirmation required.", execute)
		}
	}
	addWritePreparationTool(server, "procurement.requisitions.prepare_submit", "Prepare requisition submission", "Review the complete draft, line values, active references, current rights and exact bound context before submission.", func(ctx context.Context, input RequisitionSubmitInput) (any, []Source, []string, error) {
		return invokeProcurementRequisitionDraft(ctx, cfg, "submit", input, true)
	})
	addUpdateTool(server, "procurement.requisitions.submit", "Submit requisition for approval", "Submit an original requester-owned draft through the atomic owner workflow with exact version/context and bound confirmation.", func(ctx context.Context, input RequisitionSubmitInput) (any, []Source, []string, error) {
		return invokeProcurementRequisitionDraft(ctx, cfg, "submit", input, false)
	})
}

func invokeProcurementRequisitionDraft(ctx context.Context, cfg config.Config, op string, input any, preview bool) (any, []Source, []string, error) {
	var id int64
	body := map[string]any{}
	switch in := input.(type) {
	case RequisitionDraftInput:
		if op != "create" && op != "update" {
			return nil, nil, nil, fmt.Errorf("closed draft action required")
		}
		id = in.RequisitionID
		confirmed := in.ConfirmCreation
		if op == "update" {
			confirmed = in.ConfirmUpdate
		}
		body = map[string]any{"id": id, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "allow_duplicate": in.AllowDuplicate, "confirm_change": confirmed, "preview": preview || in.DryRun || !confirmed}
		for _, f := range []struct {
			key   string
			value *string
		}{{"title", in.Title}, {"cost_center", in.CostCenter}, {"justification", in.Justification}, {"needed_by", in.NeededBy}} {
			if f.value != nil {
				body[f.key] = *f.value
			}
		}
		if in.Lines != nil {
			body["lines"] = *in.Lines
		}
	case RequisitionSubmitInput:
		if op != "submit" {
			return nil, nil, nil, fmt.Errorf("closed submission action required")
		}
		id = in.RequisitionID
		body = map[string]any{"id": id, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirmation_text": in.ConfirmationText, "confirm_change": in.ConfirmSubmit, "preview": preview || in.DryRun || !in.ConfirmSubmit}
	default:
		return nil, nil, nil, fmt.Errorf("closed requisition action input required")
	}
	sources := []Source{{Service: "procurementcore", Entity: "requisition", ID: fmt.Sprint(id)}}
	result := map[string]any{}
	err := newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/requisitions/"+op, http.MethodPost, body, &result)
	if row, ok := result["requisition"].(map[string]any); ok {
		sources[0].ID = fmt.Sprint(row["id"])
	}
	if deps, ok := result["dependencies"].(map[string]any); ok {
		for _, spec := range []struct{ key, kind string }{{"products", "product"}, {"suppliers", "supplier"}} {
			if rows, ok := deps[spec.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok {
						sources = append(sources, Source{Service: "procurementcore", Entity: spec.kind, ID: fmt.Sprint(row["id"])})
					}
				}
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Current active owner rights and the exact consented create/update/submit scope are rechecked before every cached, legacy or restarted result. Copy the complete final version/context and bound phrase. Omitted lines retain original identities; explicit line replacements show the full diff. Business data, before/after audit, native activity and durable response commit together. Older saved business results replay without another mutation; unbooked old requests must be prepared again."), err
}

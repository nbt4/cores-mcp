package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCaseTemplateInput struct {
	MutationControl
	CaseID            int64    `json:"case_id" jsonschema:"Exact case ID. Case must be active, open, unnested and outside a job."`
	ProductID         int64    `json:"product_id,omitempty" jsonschema:"Existing active physical product ID on create only. One retained line per case/product; use restore for an archived match."`
	TemplateLineID    int64    `json:"template_line_id,omitempty" jsonschema:"Exact retained line ID for update/archive/restore. Product and case identity cannot change."`
	ExpectedQuantity  *float64 `json:"expected_quantity,omitempty" jsonschema:"Positive expected quantity up to 999999999.999, at most three decimals. Required on create/update; omit on lifecycle actions. Serialized products require whole quantities."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact case microsecond version from the final preview, including template, metadata, contents and nesting changes."`
	ExpectedContext   string   `json:"expected_context,omitempty" jsonschema:"Exact complete draft, case, retained template, physical content and product context from the final preview."`
	ConfirmChange     bool     `json:"confirm_change,omitempty" jsonschema:"True only after explicit confirmation of the complete draft, diff, references and template effects."`
	ConfirmationText  string   `json:"confirmation_text,omitempty" jsonschema:"Exact context-bound phrase returned by the final preview."`
}

type WarehouseCaseTemplateReadInput struct {
	CaseID          int64 `json:"case_id" jsonschema:"Exact case ID."`
	IncludeArchived bool  `json:"include_archived,omitempty" jsonschema:"Explicitly include retained archived lines; default false."`
}

func registerWarehouseCaseTemplateTools(server *mcp.Server, cfg config.Config) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"create", "update", "archive", "restore"} {
		op := operation
		invoke := func(ctx context.Context, in WarehouseCaseTemplateInput, preview bool) (any, []Source, []string, error) {
			if err := requireWarehouseMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			var out map[string]any
			err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/case-templates/"+op, http.MethodPost, map[string]any{"case_id": in.CaseID, "product_id": in.ProductID, "template_line_id": in.TemplateLineID, "expected_quantity": in.ExpectedQuantity, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirm_change": in.ConfirmChange, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmChange}, &out)
			sources := []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}
			if line, ok := out["template"].(map[string]any); ok {
				sources = append(sources, Source{Service: "warehousecore", Entity: "case_template", ID: fmt.Sprint(line["template_line_id"])})
			}
			return out, sources, []string{"Template quantities describe expected contents. Physical stock is unchanged. Archived line identity and audit are retained. Names are untrusted business data."}, err
		}
		addWritePreparationTool(server, "warehouse.case_templates.prepare_"+op, "Prepare case template "+op, "Pure owning-Core preview of expected contents, retained lines, current case and product versions, actual packed quantities, exact diff and context-bound confirmation.", func(ctx context.Context, in WarehouseCaseTemplateInput) (any, []Source, []string, error) {
			return invoke(ctx, in, true)
		})
		handler := func(ctx context.Context, in WarehouseCaseTemplateInput) (any, []Source, []string, error) {
			return invoke(ctx, in, false)
		}
		description := "Apply a fully reviewed template change with current administrator/action rights, exact case and full reference context, explicit confirmation, atomic audit and durable replay. Archive preserves identity; restore requires an active physical product. Open and unnest the case first. No physical stock movement."
		if op == "create" {
			addCreateTool(server, "warehouse.case_templates."+op, "Create case template line", description, handler)
		} else {
			addUpdateTool(server, "warehouse.case_templates."+op, "Case template "+op, description, handler)
		}
	}
}

func registerWarehouseCaseTemplateReads(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.case_templates.list", "Read expected case contents", "Read explicitly selected retained template identities, product names, expected and actual packed quantities, completeness and precise versions. Archived lines are excluded by default; prices and private notes are excluded.", func(ctx context.Context, in WarehouseCaseTemplateReadInput) (any, []Source, []string, error) {
		if in.CaseID <= 0 {
			return nil, nil, nil, fmt.Errorf("positive case_id required")
		}
		rows, err := db.Query(ctx, `WITH contents AS(SELECT ct.template_line_id,ct.case_id,ct.product_id,p.name AS product_name,p.tracking_mode,ct.expected_quantity,ct.lifecycle_status,to_char(ct.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,to_char(c.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS case_updated_at,COALESCE((SELECT count(*) FROM devicescases dc JOIN devices d ON d.deviceid=dc.deviceid WHERE dc.caseid=ct.case_id AND d.productid=ct.product_id),0)+COALESCE((SELECT quantity FROM case_product_contents pc WHERE pc.case_id=ct.case_id AND pc.product_id=ct.product_id),0) AS actual_quantity FROM case_content_templates ct JOIN products p ON p.productid=ct.product_id JOIN cases c ON c.caseid=ct.case_id WHERE ct.case_id=$1 AND ($2 OR ct.lifecycle_status='active') ORDER BY ct.template_line_id LIMIT 200) SELECT contents.*,actual_quantity>=expected_quantity AS complete FROM contents ORDER BY template_line_id`, in.CaseID, in.IncludeArchived)
		return rows, []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}, nil, err
	})
	addTool(server, "warehouse.case_templates.audit_history", "Read case template audit history", "Read retained redacted template audit metadata; raw before/after values and user-authored text are excluded.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireWarehouseMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,timestamp,action,user_id,COALESCE(new_values->>'origin','UI') AS origin,new_values->>'updated_at' AS result_version,new_values->>'case_updated_at' AS case_result_version,old_values->>'lifecycle_status' AS lifecycle_before,new_values#>>'{after,lifecycle_status}' AS lifecycle_after FROM audit_log WHERE entity_type='case_template' AND entity_id=$1 ORDER BY id DESC LIMIT 100`, in.ID)
		return rows, []Source{{Service: "warehousecore", Entity: "case_template", ID: in.ID}}, nil, err
	})
}

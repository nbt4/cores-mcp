package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RequisitionLineInput struct {
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
	Lines             *[]RequisitionLineInput `json:"lines,omitempty" jsonschema:"Complete line list for create or replacement on update."`
	ExpectedUpdatedAt string                  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update."`
	ConfirmCreation   bool                    `json:"confirm_creation,omitempty"`
	ConfirmUpdate     bool                    `json:"confirm_update,omitempty"`
}

type RequisitionSubmitInput struct {
	MutationControl
	RequisitionID     int64  `json:"requisition_id,omitempty"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty"`
	ConfirmSubmit     bool   `json:"confirm_submit,omitempty"`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact phrase shown by prepare_submit."`
}

func registerProcurementRequisitionTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.requisitions.prepare_create", "Prepare requisition", "Validate title, need date, every line and referenced products or suppliers, then show the full draft.", func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionDraft(ctx, db, input, false)
		return p.response("draft"), requisitionSources(input.RequisitionID, p), untrustedTextWarning(), err
	})
	addCreateTool(server, "procurement.requisitions.create", "Create requisition draft", "Create a confirmed requisition draft through ProcurementCore with target audit and durable idempotency.", func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionDraft(ctx, db, input, false)
		if err != nil || !p.Ready {
			return p.response("needs_input"), requisitionSources(0, p), nil, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), requisitionSources(0, p), []string{"No data was changed. Show the full draft and obtain explicit confirmation."}, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, "/api/v1/requisitions", http.MethodPost, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"creation_status": "created", "requisition": result}, requisitionSources(numericID(result["id"]), p), nil, nil
	})
	addWritePreparationTool(server, "procurement.requisitions.prepare_update", "Prepare requisition update", "Load an editable draft with all lines, validate replacements and show complete before/after diff and version.", func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionDraft(ctx, db, input, true)
		return p.response("draft"), requisitionSources(input.RequisitionID, p), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.requisitions.update", "Update requisition draft", "Replace confirmed fields and lines of an owned draft after full diff and exact version.", func(ctx context.Context, input RequisitionDraftInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionDraft(ctx, db, input, true)
		if err != nil || !p.Ready {
			return p.response("needs_input"), requisitionSources(input.RequisitionID, p), nil, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), requisitionSources(input.RequisitionID, p), []string{"No data was changed. Show the full diff and obtain explicit confirmation."}, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/requisitions/%d", input.RequisitionID), http.MethodPut, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "requisition": result, "diff": p.Diff}, requisitionSources(input.RequisitionID, p), nil, nil
	})
	addWritePreparationTool(server, "procurement.requisitions.prepare_submit", "Prepare requisition submission", "Show the complete draft, line values, current version and record-bound confirmation phrase before submission.", func(ctx context.Context, input RequisitionSubmitInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionSubmit(ctx, db, input)
		return p.response("draft"), requisitionSources(input.RequisitionID, p), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.requisitions.submit", "Submit requisition for approval", "Submit one owned draft after exact version, complete preview and elevated confirmation.", func(ctx context.Context, input RequisitionSubmitInput) (any, []Source, []string, error) {
		p, err := prepareRequisitionSubmit(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), requisitionSources(input.RequisitionID, p), nil, err
		}
		if !input.ConfirmSubmit {
			return p.response("confirmation_required"), requisitionSources(input.RequisitionID, p), []string{"No data was changed."}, nil
		}
		if strings.TrimSpace(input.ConfirmationText) != fmt.Sprintf("SUBMIT REQUISITION %d", input.RequisitionID) {
			return p.response("elevated_confirmation_required"), requisitionSources(input.RequisitionID, p), []string{"No data was changed. Type the phrase shown by prepare_submit."}, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/requisitions/%d/submit", input.RequisitionID), http.MethodPost, map[string]any{"expectedUpdatedAt": input.ExpectedUpdatedAt}, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "submitted", "requisition": result}, requisitionSources(input.RequisitionID, p), nil, nil
	})
}

func requisitionSources(id int64, p preparedMutation) []Source {
	entity := "requisition_draft"
	if id > 0 {
		entity = "requisition"
	}
	sources := []Source{{Service: "procurementcore", Entity: entity, ID: fmt.Sprint(id)}}
	for _, row := range p.RelatedRecords {
		if productID := numericID(row["product_id"]); productID > 0 {
			sources = append(sources, Source{Service: "procurementcore", Entity: "product", ID: fmt.Sprint(productID)})
		}
		if supplierID := numericID(row["supplier_id"]); supplierID > 0 {
			sources = append(sources, Source{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(supplierID)})
		}
	}
	return sources
}

func requisitionOwner(ctx context.Context, row map[string]any) bool {
	info := auth.TokenInfoFromContext(ctx)
	return info != nil && (info.Extra["is_admin"] == true || info.UserID == fmt.Sprint(row["requester_id"]))
}

func loadRequisition(ctx context.Context, db *store.Store, id int64) (map[string]any, []map[string]any, error) {
	rows, err := db.Query(ctx, `SELECT id,number,title,status,requester_id,cost_center,justification,needed_by,estimated_total_cents,to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_requisitions WHERE id=$1`, id)
	if err != nil || len(rows) == 0 {
		return nil, nil, err
	}
	lines, err := db.Query(ctx, `SELECT id,product_id,description,quantity,unit,estimated_price_cents,preferred_supplier_id,purchase_url FROM proc_requisition_lines WHERE requisition_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, nil, err
	}
	return rows[0], lines, nil
}

func lineDraft(line RequisitionLineInput) map[string]any {
	var productID, supplierID any
	if line.ProductID != nil {
		productID = *line.ProductID
	}
	if line.PreferredSupplierID != nil {
		supplierID = *line.PreferredSupplierID
	}
	return map[string]any{"productId": productID, "description": strings.TrimSpace(line.Description), "quantity": line.Quantity, "unit": strings.TrimSpace(line.Unit), "estimatedPriceCents": line.EstimatedPriceCents, "preferredSupplierId": supplierID, "purchaseUrl": strings.TrimSpace(line.PurchaseURL)}
}

func prepareRequisitionDraft(ctx context.Context, db *store.Store, input RequisitionDraftInput, update bool) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if update {
		if input.RequisitionID <= 0 {
			p.require("requisition_id", "Welcher Entwurf soll bearbeitet werden?", nil)
			p.finish()
			return p, nil
		}
		row, lines, err := loadRequisition(ctx, db, input.RequisitionID)
		if err != nil {
			return p, err
		}
		if row == nil {
			p.require("requisition_id", "Der Entwurf wurde nicht gefunden.", nil)
			p.finish()
			return p, nil
		}
		if !requisitionOwner(ctx, row) {
			return p, fmt.Errorf("only the requester or a procurement administrator may edit this requisition")
		}
		if fmt.Sprint(row["status"]) != "draft" {
			p.require("status", "Nur Entwürfe können geändert werden.", row["status"])
		}
		currentLines := make([]map[string]any, 0, len(lines))
		for _, line := range lines {
			currentLines = append(currentLines, map[string]any{"productId": line["product_id"], "description": nullableText(line["description"]), "quantity": numericFloat(line["quantity"]), "unit": nullableText(line["unit"]), "estimatedPriceCents": numericID(line["estimated_price_cents"]), "preferredSupplierId": line["preferred_supplier_id"], "purchaseUrl": nullableText(line["purchase_url"])})
		}
		p.Current = map[string]any{"title": nullableText(row["title"]), "costCenter": nullableText(row["cost_center"]), "justification": nullableText(row["justification"]), "neededBy": row["needed_by"], "lines": currentLines, "expectedUpdatedAt": rfc3339Value(row["updated_at"])}
		p.Draft = cloneMap(p.Current)
	} else if input.RequisitionID != 0 {
		p.require("requisition_id", "Bei der Neuanlage darf keine vorhandene ID angegeben werden.", nil)
	}
	for _, field := range []struct {
		key   string
		value *string
	}{{"title", input.Title}, {"costCenter", input.CostCenter}, {"justification", input.Justification}} {
		if field.value != nil {
			p.Draft[field.key] = strings.TrimSpace(*field.value)
		}
	}
	if input.NeededBy != nil {
		p.Draft["neededBy"] = strings.TrimSpace(*input.NeededBy)
		if *input.NeededBy == "" {
			p.Draft["neededBy"] = nil
		}
	}
	if input.Lines != nil {
		lines := make([]map[string]any, 0, len(*input.Lines))
		for _, line := range *input.Lines {
			lines = append(lines, lineDraft(line))
		}
		p.Draft["lines"] = lines
	}
	if !update {
		delete(p.Draft, "expectedUpdatedAt")
	}
	if update {
		p.Diff = map[string]map[string]any{}
		for _, key := range []string{"title", "costCenter", "justification", "neededBy", "lines"} {
			if !reflect.DeepEqual(p.Current[key], p.Draft[key]) {
				p.Diff[key] = map[string]any{"before": p.Current[key], "after": p.Draft[key]}
			}
		}
		if len(p.Diff) == 0 {
			p.require("changes", "Welche Felder oder Positionen sollen geändert werden?", nil)
		}
		version := fmt.Sprint(p.Current["expectedUpdatedAt"])
		if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
			p.require("expected_updated_at", "Die Version aus der Vorschau muss übernommen werden.", version)
		} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
			p.require("expected_updated_at", "Der Entwurf wurde geändert. Bitte erneut vorbereiten.", version)
		}
	}
	if title := nullableText(p.Draft["title"]); title == "" || len([]rune(title)) > 240 {
		p.require("title", "Welcher Titel (maximal 240 Zeichen) beschreibt den Bedarf?", nil)
	}
	if len([]rune(nullableText(p.Draft["costCenter"]))) > 80 {
		p.require("cost_center", "Die Kostenstelle darf höchstens 80 Zeichen haben.", nil)
	}
	if date := nullableText(p.Draft["neededBy"]); date != "" {
		parsed, err := time.Parse(time.RFC3339, date)
		if err != nil {
			p.require("needed_by", "Datum im RFC3339-Format angeben.", nil)
		} else {
			p.Draft["neededBy"] = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	lines, ok := p.Draft["lines"].([]map[string]any)
	if !ok || len(lines) == 0 {
		p.require("lines", "Mindestens eine Position ist erforderlich.", nil)
	} else if len(lines) > 100 {
		p.require("lines", "Höchstens 100 Positionen pro Bedarf.", nil)
	}
	for index, line := range lines {
		field := fmt.Sprintf("lines[%d]", index)
		if desc := nullableText(line["description"]); desc == "" || len([]rune(desc)) > 500 {
			p.require(field+".description", "Beschreibung mit höchstens 500 Zeichen angeben.", nil)
		}
		quantity := numericFloat(line["quantity"])
		if quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
			p.require(field+".quantity", "Eine endliche positive Menge angeben.", nil)
		}
		if unit := nullableText(line["unit"]); unit == "" {
			line["unit"] = "Stk."
		} else if len([]rune(unit)) > 30 {
			p.require(field+".unit", "Einheit mit höchstens 30 Zeichen angeben.", nil)
		}
		if numericFloat(line["estimatedPriceCents"]) < 0 {
			p.require(field+".estimated_price_cents", "Preis in Cent darf nicht negativ sein.", nil)
		}
		if quantity > 1e9 || numericFloat(line["estimatedPriceCents"]) > 1e9 {
			p.require(field+".quantity", "Menge oder Schätzwert ist zu groß.", nil)
		}
		if len(nullableText(line["purchaseUrl"])) > 2000 {
			p.require(field+".purchase_url", "Kauflink ist zu lang.", nil)
		}
		for _, reference := range []struct{ key, table, label string }{{"productId", "proc_products", "product_id"}, {"preferredSupplierId", "proc_suppliers", "preferred_supplier_id"}} {
			if line[reference.key] == nil {
				continue
			}
			id := numericID(line[reference.key])
			if id <= 0 {
				p.require(field+"."+reference.label, "Eine vorhandene positive ID angeben oder das Feld weglassen.", nil)
				continue
			}
			query := `SELECT id,name FROM ` + reference.table + ` WHERE id=$1 AND active=true`
			rows, err := db.Query(ctx, query, id)
			if err != nil {
				return p, err
			}
			if len(rows) != 1 {
				p.require(field+"."+reference.label, "Referenz ist nicht aktiv oder existiert nicht.", nil)
			} else {
				p.RelatedRecords = append(p.RelatedRecords, map[string]any{strings.TrimSuffix(reference.label, "_id") + "_id": id, "name": rows[0]["name"]})
			}
		}
	}
	p.finish()
	return p, nil
}

func prepareRequisitionSubmit(ctx context.Context, db *store.Store, input RequisitionSubmitInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.RequisitionID <= 0 {
		p.require("requisition_id", "Welcher Entwurf soll eingereicht werden?", nil)
		p.finish()
		return p, nil
	}
	row, lines, err := loadRequisition(ctx, db, input.RequisitionID)
	if err != nil {
		return p, err
	}
	if row == nil {
		p.require("requisition_id", "Entwurf wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	if !requisitionOwner(ctx, row) {
		return p, fmt.Errorf("only the requester or a procurement administrator may submit this requisition")
	}
	if fmt.Sprint(row["status"]) != "draft" {
		p.require("status", "Nur Entwürfe können eingereicht werden.", row["status"])
	}
	if len(lines) == 0 {
		p.require("lines", "Ein Bedarf benötigt mindestens eine Position.", nil)
	}
	for index, line := range lines {
		for _, reference := range []struct{ key, table string }{{"product_id", "proc_products"}, {"preferred_supplier_id", "proc_suppliers"}} {
			if line[reference.key] == nil {
				continue
			}
			id := numericID(line[reference.key])
			rows, queryErr := db.Query(ctx, `SELECT id FROM `+reference.table+` WHERE id=$1 AND active=true`, id)
			if queryErr != nil {
				return p, queryErr
			}
			if len(rows) != 1 {
				p.require(fmt.Sprintf("lines[%d].%s", index, reference.key), "Referenz ist nicht mehr aktiv. Entwurf zuerst korrigieren.", nil)
			}
		}
	}
	version := rfc3339Value(row["updated_at"])
	if input.ConfirmSubmit && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Version aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Entwurf wurde seit der Vorschau geändert.", version)
	}
	p.Current = row
	p.RelatedRecords = lines
	p.Draft = map[string]any{"requisition_id": input.RequisitionID, "title": row["title"], "status_after": "submitted", "lines": lines, "estimated_total_cents": row["estimated_total_cents"], "expected_updated_at": version, "confirmation_text_required": fmt.Sprintf("SUBMIT REQUISITION %d", input.RequisitionID)}
	p.finish()
	return p, nil
}

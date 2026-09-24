package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type SupplierUpdateInput struct {
	MutationControl
	SupplierID        int64    `json:"supplier_id,omitempty" jsonschema:"Exact ProcurementCore supplier ID."`
	Name              *string  `json:"name,omitempty" jsonschema:"Optional replacement name; at most 180 characters."`
	Code              *string  `json:"code,omitempty" jsonschema:"Optional replacement code; unique, at most 40 uppercase letters, digits, dots, underscores or hyphens."`
	Website           *string  `json:"website,omitempty" jsonschema:"Optional HTTP(S) website; empty string clears it."`
	ContactName       *string  `json:"contact_name,omitempty" jsonschema:"Optional business contact name; at most 160 characters."`
	Email             *string  `json:"email,omitempty" jsonschema:"Optional business email address; empty string clears it."`
	Phone             *string  `json:"phone,omitempty" jsonschema:"Optional business phone number; at most 80 characters."`
	PaymentTerms      *string  `json:"payment_terms,omitempty" jsonschema:"Optional payment terms; at most 120 characters."`
	DefaultLeadDays   *int     `json:"default_lead_days,omitempty" jsonschema:"Optional lead time from 0 to 36500 days."`
	Rating            *float64 `json:"rating,omitempty" jsonschema:"Optional rating from 0 to 5."`
	Preferred         *bool    `json:"preferred,omitempty" jsonschema:"Optional preferred supplier flag."`
	Active            *bool    `json:"active,omitempty" jsonschema:"Set false to deactivate or archive the supplier without deleting its orders and offers."`
	RiskLevel         *string  `json:"risk_level,omitempty" jsonschema:"Optional risk level: low, medium or high."`
	Notes             *string  `json:"notes,omitempty" jsonschema:"Optional internal notes; at most 2000 characters."`
	AllowSimilar      bool     `json:"allow_similar,omitempty" jsonschema:"Set true only after reviewing similar suppliers for a renamed company."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool     `json:"confirm_update,omitempty" jsonschema:"Set true only after showing the full before/after diff and receiving explicit confirmation."`
}

func registerSupplierUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.suppliers.prepare_update", "Prepare supplier update", "Load the complete supplier for an authorized Procurement administrator, validate changed fields, detect duplicate codes and similar names, and show a before/after diff with a precise version.", func(ctx context.Context, input SupplierUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareSupplierUpdate(ctx, db, input)
		return prepared.response("draft"), supplierUpdateSources(prepared, input.SupplierID), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.suppliers.update", "Update supplier", "Update a ProcurementCore supplier after prepare_update, complete diff, unchanged version and explicit confirmation. Setting active=false deactivates the supplier without deleting its history.", func(ctx context.Context, input SupplierUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareSupplierUpdate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), supplierUpdateSources(prepared, input.SupplierID), append(prepared.Warnings, "No data was changed."), err
		}
		if !input.ConfirmUpdate {
			return prepared.response("confirmation_required"), supplierUpdateSources(prepared, input.SupplierID), []string{"No data was changed. Show the complete before/after diff and obtain explicit confirmation."}, nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/suppliers/%d", input.SupplierID), http.MethodPut, prepared.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "supplier": updated, "diff": prepared.Diff}, []Source{{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(input.SupplierID)}}, nil, nil
	})
}

func supplierUpdateSources(prepared preparedMutation, id int64) []Source {
	result := []Source{{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(id)}}
	for _, record := range prepared.RelatedRecords {
		if orderID, ok := record["order_id"]; ok {
			result = append(result, Source{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(orderID)})
		} else if supplierID, ok := record["id"]; ok {
			result = append(result, Source{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(supplierID)})
		}
	}
	return result
}

func prepareSupplierUpdate(ctx context.Context, db *store.Store, input SupplierUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.SupplierID <= 0 {
		p.require("supplier_id", "Welche gültige Lieferanten-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to read and update supplier details")
	}
	rows, err := db.Query(ctx, `SELECT id,name,code,website,contact_name,email,phone,payment_terms,default_lead_days,rating,preferred,active,risk_level,notes,
		to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at
		FROM proc_suppliers WHERE id=$1`, input.SupplierID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("supplier_id", "Der Lieferant wurde nicht gefunden. Welche gültige ID soll verwendet werden?", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	currentVersion := rfc3339Value(p.Current["updated_at"])
	p.Draft = map[string]any{
		"name": fmt.Sprint(p.Current["name"]), "code": fmt.Sprint(p.Current["code"]),
		"website": nullableText(p.Current["website"]), "contactName": nullableText(p.Current["contact_name"]),
		"email": nullableText(p.Current["email"]), "phone": nullableText(p.Current["phone"]),
		"paymentTerms":    nullableText(p.Current["payment_terms"]),
		"defaultLeadDays": int(numericID(p.Current["default_lead_days"])), "rating": numericFloat(p.Current["rating"]),
		"preferred": p.Current["preferred"], "active": p.Current["active"],
		"riskLevel": nullableText(p.Current["risk_level"]), "notes": nullableText(p.Current["notes"]),
		"expectedUpdatedAt": currentVersion,
	}
	before := cloneMap(p.Draft)
	setString := func(key string, value *string) {
		if value != nil {
			p.Draft[key] = strings.TrimSpace(*value)
		}
	}
	setString("name", input.Name)
	if input.Code != nil {
		p.Draft["code"] = strings.ToUpper(strings.TrimSpace(*input.Code))
	}
	setString("website", input.Website)
	setString("contactName", input.ContactName)
	setString("email", input.Email)
	setString("phone", input.Phone)
	setString("paymentTerms", input.PaymentTerms)
	setString("riskLevel", input.RiskLevel)
	setString("notes", input.Notes)
	if input.DefaultLeadDays != nil {
		p.Draft["defaultLeadDays"] = *input.DefaultLeadDays
	}
	if input.Rating != nil {
		p.Draft["rating"] = *input.Rating
	}
	if input.Preferred != nil {
		p.Draft["preferred"] = *input.Preferred
	}
	if input.Active != nil {
		p.Draft["active"] = *input.Active
	}
	p.Diff = map[string]map[string]any{}
	for field, previous := range before {
		if field != "expectedUpdatedAt" && !reflect.DeepEqual(previous, p.Draft[field]) {
			p.Diff[field] = map[string]any{"before": previous, "after": p.Draft[field]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Lieferantenfelder sollen tatsächlich geändert werden?", nil)
	}
	name, code := fmt.Sprint(p.Draft["name"]), fmt.Sprint(p.Draft["code"])
	if name == "" || len([]rune(name)) > 180 {
		p.require("name", "Welcher gültige Lieferantenname mit höchstens 180 Zeichen soll gespeichert werden?", nil)
	}
	if !supplierCodePattern.MatchString(code) {
		p.require("code", "Welcher gültige Lieferantencode mit höchstens 40 Zeichen soll gespeichert werden?", nil)
	}
	if website := fmt.Sprint(p.Draft["website"]); website != "" {
		parsed, parseErr := url.ParseRequestURI(website)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || len(website) > 1000 {
			p.require("website", "Welche gültige HTTP(S)-Website soll gespeichert werden?", nil)
		}
	}
	if email := fmt.Sprint(p.Draft["email"]); email != "" {
		parsed, parseErr := mail.ParseAddress(email)
		if parseErr != nil || parsed.Address != email || len(email) > 255 {
			p.require("email", "Welche gültige geschäftliche E-Mail-Adresse soll gespeichert werden?", nil)
		}
	}
	for _, field := range []struct {
		key string
		max int
	}{{"contactName", 160}, {"phone", 80}, {"paymentTerms", 120}, {"notes", 2000}} {
		if len([]rune(fmt.Sprint(p.Draft[field.key]))) > field.max {
			p.require(field.key, fmt.Sprintf("Bitte %s auf höchstens %d Zeichen kürzen.", field.key, field.max), nil)
		}
	}
	if days := p.Draft["defaultLeadDays"].(int); days < 0 || days > 36500 {
		p.require("default_lead_days", "Welche Standardlieferzeit zwischen 0 und 36500 Tagen gilt?", nil)
	}
	if rating := p.Draft["rating"].(float64); rating < 0 || rating > 5 {
		p.require("rating", "Welche Bewertung zwischen 0 und 5 gilt?", nil)
	}
	if risk := fmt.Sprint(p.Draft["riskLevel"]); risk != "low" && risk != "medium" && risk != "high" {
		p.require("risk_level", "Welche Risikostufe gilt: low, medium oder high?", nil)
	}
	if before["active"] == true && p.Draft["active"] == false {
		orders, queryErr := db.Query(ctx, `SELECT id AS order_id,number,status FROM proc_purchase_orders WHERE supplier_id=$1 AND status NOT IN ('cancelled','received') ORDER BY id LIMIT 50`, input.SupplierID)
		if queryErr != nil {
			return p, queryErr
		}
		if len(orders) > 0 {
			p.RelatedRecords = append(p.RelatedRecords, orders...)
			p.require("open_orders", "Offene Bestellungen müssen vor der Deaktivierung abgeschlossen oder storniert werden.", orders)
		}
	}
	if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die genaue Version aus der aktuellen Vorschau muss übernommen werden.", currentVersion)
	} else if input.ExpectedUpdatedAt != "" && strings.TrimSpace(input.ExpectedUpdatedAt) != currentVersion {
		p.require("expected_updated_at", "Der Lieferant wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", currentVersion)
	}
	if len(strings.Fields(name)) > 0 && supplierCodePattern.MatchString(code) && (input.Name != nil || input.Code != nil) {
		firstWord := strings.Fields(name)[0]
		candidates, queryErr := db.Query(ctx, `SELECT id,name,code,active FROM proc_suppliers
			WHERE id<>$1 AND (lower(code)=lower($2) OR lower(name)=lower($3) OR position(lower($4) in lower(name)) > 0)
			ORDER BY CASE WHEN lower(code)=lower($2) THEN 0 WHEN lower(name)=lower($3) THEN 1 ELSE 2 END,name LIMIT 100`, input.SupplierID, code, name, firstWord)
		if queryErr != nil {
			return p, queryErr
		}
		var similar []map[string]any
		for _, candidate := range candidates {
			if strings.EqualFold(fmt.Sprint(candidate["code"]), code) {
				p.RelatedRecords = append(p.RelatedRecords, candidate)
				p.require("duplicate_code", "Der neue Code gehört bereits zu einem anderen Lieferanten.", candidate)
			} else if warehouseMatchScore(name, fmt.Sprint(candidate["name"]), "") >= 50 {
				p.RelatedRecords = append(p.RelatedRecords, candidate)
				similar = append(similar, candidate)
			}
		}
		if len(candidates) >= 100 {
			p.require("supplier_search_limit", "Die Ähnlichkeitssuche ist unvollständig. Bitte den Lieferantenbestand gezielt prüfen.", nil)
		}
		if len(similar) > 0 && !input.AllowSimilar {
			p.require("similar_supplier_review", "Ist die umbenannte Firma trotz ähnlicher Bestandsnamen eindeutig dieselbe Lieferantenidentität?", similar)
		}
	}
	p.finish()
	return p, nil
}

func nullableText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

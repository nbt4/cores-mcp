package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

var supplierCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,39}$`)

type SupplierCreateInput struct {
	MutationControl
	Name            string  `json:"name,omitempty" jsonschema:"Required supplier name, at most 180 characters."`
	Code            string  `json:"code,omitempty" jsonschema:"Required unique supplier code, at most 40 uppercase letters, digits, dots, underscores or hyphens."`
	Website         string  `json:"website,omitempty" jsonschema:"Optional HTTP(S) supplier website."`
	ContactName     string  `json:"contact_name,omitempty" jsonschema:"Optional business contact name; at most 160 characters."`
	Email           string  `json:"email,omitempty" jsonschema:"Optional business contact email address."`
	Phone           string  `json:"phone,omitempty" jsonschema:"Optional business phone number; at most 80 characters."`
	PaymentTerms    string  `json:"payment_terms,omitempty" jsonschema:"Optional payment terms; at most 120 characters."`
	DefaultLeadDays int     `json:"default_lead_days,omitempty" jsonschema:"Default lead time from 0 to 36500 days."`
	Rating          float64 `json:"rating,omitempty" jsonschema:"Supplier rating between 0 and 5."`
	Preferred       bool    `json:"preferred,omitempty"`
	Active          *bool   `json:"active,omitempty" jsonschema:"Defaults to true."`
	RiskLevel       string  `json:"risk_level,omitempty" jsonschema:"One of low, medium, or high; defaults to low."`
	Notes           string  `json:"notes,omitempty" jsonschema:"Optional internal supplier notes; at most 2000 characters."`
	AllowSimilar    bool    `json:"allow_similar,omitempty" jsonschema:"Set true only after the user reviewed similar suppliers and confirmed this is a distinct company."`
	ConfirmCreation bool    `json:"confirm_creation,omitempty" jsonschema:"Set true only after the complete final draft was shown and the user explicitly confirmed creation."`
}

func registerSupplierCreateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.suppliers.prepare_create", "Prepare procurement supplier", "Validate all supplier fields, check unique code and similar names, and return a complete draft with precise questions. No data is changed.", func(ctx context.Context, input SupplierCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareSupplierCreate(ctx, db, input)
		return prepared.response("draft"), supplierPreparationSources(prepared), untrustedTextWarning(), err
	})
	addCreateTool(server, "procurement.suppliers.create", "Create procurement supplier", "Create one ProcurementCore supplier after prepare_create, duplicate review, final draft, and explicit user confirmation. ProcurementCore commits supplier, audit, activity and idempotency result together.", func(ctx context.Context, input SupplierCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareSupplierCreate(ctx, db, input)
		if err != nil {
			return nil, nil, nil, err
		}
		if !prepared.Ready {
			return prepared.response("needs_input"), supplierPreparationSources(prepared), []string{"No data was changed. Ask every listed question."}, nil
		}
		if !input.ConfirmCreation {
			return prepared.response("confirmation_required"), supplierPreparationSources(prepared), []string{"No data was changed. Show the complete supplier draft and obtain explicit confirmation."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, "/api/v1/suppliers", http.MethodPost, prepared.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"creation_status": "created", "supplier": created}, []Source{{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(created["id"])}}, nil, nil
	})
}

func supplierPreparationSources(prepared preparedOperationalCreate) []Source {
	return append([]Source{{Service: "procurementcore", Entity: "supplier_draft"}}, prepared.Sources...)
}

func prepareSupplierCreate(ctx context.Context, db *store.Store, input SupplierCreateInput) (preparedOperationalCreate, error) {
	name := strings.TrimSpace(input.Name)
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	website := strings.TrimSpace(input.Website)
	email := strings.TrimSpace(input.Email)
	risk := strings.ToLower(strings.TrimSpace(input.RiskLevel))
	if risk == "" {
		risk = "low"
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	prepared := preparedOperationalCreate{Draft: map[string]any{
		"name": name, "code": code, "website": website,
		"contactName": strings.TrimSpace(input.ContactName), "email": email,
		"phone": strings.TrimSpace(input.Phone), "paymentTerms": strings.TrimSpace(input.PaymentTerms),
		"defaultLeadDays": input.DefaultLeadDays, "rating": input.Rating,
		"preferred": input.Preferred, "active": active, "riskLevel": risk,
		"notes": strings.TrimSpace(input.Notes),
	}}
	if name == "" || len([]rune(name)) > 180 {
		prepared.require("name", "Wie lautet der Lieferantenname mit höchstens 180 Zeichen?")
	}
	if !supplierCodePattern.MatchString(code) {
		prepared.require("code", "Welcher eindeutige Lieferantencode aus höchstens 40 Buchstaben, Ziffern, Punkt, Unterstrich oder Bindestrich soll verwendet werden?")
	}
	if website != "" {
		parsed, err := url.ParseRequestURI(website)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || len(website) > 1000 {
			prepared.require("website", "Welche gültige HTTP(S)-Website mit höchstens 1000 Zeichen gehört zum Lieferanten?")
		}
	}
	if email != "" {
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || len(email) > 255 {
			prepared.require("email", "Welche gültige geschäftliche E-Mail-Adresse gehört zum Lieferanten?")
		}
	}
	for _, field := range []struct {
		name, value string
		maximum     int
	}{
		{"contact_name", input.ContactName, 160}, {"phone", input.Phone, 80},
		{"payment_terms", input.PaymentTerms, 120}, {"notes", input.Notes, 2000},
	} {
		if len([]rune(strings.TrimSpace(field.value))) > field.maximum {
			prepared.require(field.name, fmt.Sprintf("Bitte %s auf höchstens %d Zeichen kürzen.", field.name, field.maximum))
		}
	}
	if input.DefaultLeadDays < 0 || input.DefaultLeadDays > 36500 {
		prepared.require("default_lead_days", "Welche Standardlieferzeit zwischen 0 und 36500 Tagen gilt?")
	}
	if input.Rating < 0 || input.Rating > 5 {
		prepared.require("rating", "Welche Bewertung zwischen 0 und 5 gilt?")
	}
	if risk != "low" && risk != "medium" && risk != "high" {
		prepared.require("risk_level", "Welche Risikostufe gilt: low, medium oder high?")
	}
	if len(prepared.Missing) > 0 {
		return prepared, nil
	}
	firstWord := strings.Fields(name)[0]
	rows, err := db.Query(ctx, `SELECT id,name,code,active FROM proc_suppliers
		WHERE lower(code)=lower($1) OR lower(name)=lower($2) OR position(lower($3) in lower(name)) > 0
		ORDER BY CASE WHEN lower(code)=lower($1) THEN 0 WHEN lower(name)=lower($2) THEN 1 ELSE 2 END,name LIMIT 100`, code, name, firstWord)
	if err != nil {
		return prepared, err
	}
	var similar []map[string]any
	var related []map[string]any
	for _, row := range rows {
		if strings.EqualFold(fmt.Sprint(row["code"]), code) {
			related = append(related, row)
			prepared.require("duplicate_code", "Dieser Lieferantencode existiert bereits. Bitte vorhandenen Lieferanten verwenden oder einen anderen Code wählen.")
			continue
		}
		if warehouseMatchScore(name, fmt.Sprint(row["name"]), "") >= 50 {
			similar = append(similar, row)
			related = append(related, row)
		}
	}
	prepared.RelatedRecords = related
	if len(related) > 0 {
		prepared.Sources = sourcesFor("procurementcore", "supplier", related)
	}
	if len(rows) >= 100 {
		prepared.require("supplier_search_limit", "Die Suche hat mindestens 100 mögliche Lieferanten gefunden. Bitte den Lieferantenbestand gezielt prüfen, bevor ein neuer Datensatz angelegt wird.")
	}
	if len(similar) > 0 && !input.AllowSimilar {
		prepared.Missing = append(prepared.Missing, "similar_supplier_review")
		prepared.Questions = append(prepared.Questions, question("similar_supplier_review", "Ähnliche Lieferanten existieren bereits. Ist die Neuanlage wirklich eine andere Firma?", "required", similar))
	}
	prepared.Ready = len(prepared.Missing) == 0
	return prepared, nil
}

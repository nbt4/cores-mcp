package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type OfferCreateInput struct {
	MutationControl
	ProductID       int64   `json:"product_id,omitempty" jsonschema:"Exact active ProcurementCore product ID."`
	SupplierID      int64   `json:"supplier_id,omitempty" jsonschema:"Exact active ProcurementCore supplier ID."`
	SupplierSKU     string  `json:"supplier_sku,omitempty"`
	PriceCents      *int64  `json:"price_cents,omitempty" jsonschema:"Required non-negative price in integer cents."`
	Currency        string  `json:"currency,omitempty" jsonschema:"Three-letter currency; defaults to EUR."`
	MinimumQuantity float64 `json:"minimum_quantity,omitempty" jsonschema:"Positive minimum quantity; defaults to 1."`
	PackSize        float64 `json:"pack_size,omitempty" jsonschema:"Positive pack size; defaults to 1."`
	LeadDays        int     `json:"lead_days,omitempty" jsonschema:"Non-negative delivery time in days."`
	PurchaseURL     string  `json:"purchase_url,omitempty" jsonschema:"Optional HTTP(S) product URL."`
	ValidUntil      string  `json:"valid_until,omitempty" jsonschema:"Optional RFC3339 expiry date."`
	AllowDuplicate  bool    `json:"allow_duplicate,omitempty" jsonschema:"Set only after reviewing an existing offer for the same product and supplier."`
	ConfirmCreation bool    `json:"confirm_creation,omitempty" jsonschema:"Set only after showing the final offer draft and receiving explicit confirmation."`
}

type OfferUpdateInput struct {
	MutationControl
	OfferID           int64    `json:"offer_id,omitempty" jsonschema:"Exact existing ProcurementCore offer ID."`
	SupplierID        *int64   `json:"supplier_id,omitempty" jsonschema:"Replacement active supplier ID."`
	SupplierSKU       *string  `json:"supplier_sku,omitempty"`
	PriceCents        *int64   `json:"price_cents,omitempty" jsonschema:"Non-negative price in integer cents."`
	Currency          *string  `json:"currency,omitempty" jsonschema:"Three-letter currency."`
	MinimumQuantity   *float64 `json:"minimum_quantity,omitempty" jsonschema:"Positive minimum quantity."`
	PackSize          *float64 `json:"pack_size,omitempty" jsonschema:"Positive pack size."`
	LeadDays          *int     `json:"lead_days,omitempty" jsonschema:"Non-negative delivery time in days."`
	PurchaseURL       *string  `json:"purchase_url,omitempty" jsonschema:"HTTP(S) URL; empty string clears."`
	ValidUntil        *string  `json:"valid_until,omitempty" jsonschema:"RFC3339 date; empty string clears."`
	Active            *bool    `json:"active,omitempty" jsonschema:"False archives the offer; true restores it."`
	AllowDuplicate    bool     `json:"allow_duplicate,omitempty" jsonschema:"Set only after reviewing another offer for the same product and supplier."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update."`
	ConfirmUpdate     bool     `json:"confirm_update,omitempty" jsonschema:"Set only after showing the full before/after diff and receiving explicit confirmation."`
}

func registerProcurementOfferTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.offers.prepare_create", "Prepare supplier offer", "Validate product, supplier, price, dates and existing offers before adding a supplier offer.", func(ctx context.Context, input OfferCreateInput) (any, []Source, []string, error) {
		p, err := prepareOfferCreate(ctx, db, input)
		return p.response("draft"), offerSources(input.ProductID, input.SupplierID, 0), untrustedTextWarning(), err
	})
	addCreateTool(server, "procurement.offers.create", "Create supplier offer", "Create one confirmed offer through ProcurementCore with target-Core audit and durable idempotency.", func(ctx context.Context, input OfferCreateInput) (any, []Source, []string, error) {
		p, err := prepareOfferCreate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), offerSources(input.ProductID, input.SupplierID, 0), nil, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), offerSources(input.ProductID, input.SupplierID, 0), []string{"No data was changed. Show the final draft and obtain explicit confirmation."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/products/%d/offers", input.ProductID), http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"creation_status": "created", "offer": created}, offerSources(input.ProductID, input.SupplierID, numericID(created["id"])), nil, nil
	})
	addWritePreparationTool(server, "procurement.offers.prepare_update", "Prepare supplier offer update", "Load an offer, validate changes and show the complete diff and exact version. active=false archives; active=true restores.", func(ctx context.Context, input OfferUpdateInput) (any, []Source, []string, error) {
		p, err := prepareOfferUpdate(ctx, db, input)
		return p.response("draft"), offerSources(numericID(p.Draft["productId"]), numericID(p.Draft["supplierId"]), input.OfferID), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.offers.update", "Update supplier offer", "Update or archive an offer after complete diff, unchanged version and explicit confirmation.", func(ctx context.Context, input OfferUpdateInput) (any, []Source, []string, error) {
		p, err := prepareOfferUpdate(ctx, db, input)
		sources := offerSources(numericID(p.Draft["productId"]), numericID(p.Draft["supplierId"]), input.OfferID)
		if err != nil || !p.Ready {
			return p.response("needs_input"), sources, []string{"No data was changed."}, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), sources, []string{"No data was changed. Show the diff and obtain explicit confirmation."}, nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/offers/%d", input.OfferID), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "offer": updated, "diff": p.Diff}, sources, nil, nil
	})
}

func offerSources(productID, supplierID, offerID int64) []Source {
	sources := []Source{}
	if productID > 0 {
		sources = append(sources, Source{Service: "procurementcore", Entity: "product", ID: fmt.Sprint(productID)})
	}
	if supplierID > 0 {
		sources = append(sources, Source{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(supplierID)})
	}
	if offerID > 0 {
		sources = append(sources, Source{Service: "procurementcore", Entity: "offer", ID: fmt.Sprint(offerID)})
	}
	if len(sources) == 0 {
		return []Source{{Service: "procurementcore", Entity: "offer_draft"}}
	}
	return sources
}

func requireProcurementAdmin(ctx context.Context) error {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return fmt.Errorf("Procurement administrator permission is required for supplier offers")
	}
	return nil
}

func prepareOfferCreate(ctx context.Context, db *store.Store, input OfferCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireProcurementAdmin(ctx); err != nil {
		return p, err
	}
	p.Draft = map[string]any{
		"supplierId": input.SupplierID, "supplierSku": strings.TrimSpace(input.SupplierSKU), "currency": strings.ToUpper(strings.TrimSpace(input.Currency)),
		"minimumQuantity": input.MinimumQuantity, "packSize": input.PackSize, "leadDays": input.LeadDays,
		"purchaseUrl": strings.TrimSpace(input.PurchaseURL), "active": true,
	}
	if input.PriceCents != nil {
		p.Draft["priceCents"] = *input.PriceCents
	}
	if input.ValidUntil != "" {
		p.Draft["validUntil"] = strings.TrimSpace(input.ValidUntil)
	}
	if p.Draft["currency"] == "" {
		p.Draft["currency"] = "EUR"
	}
	if input.MinimumQuantity == 0 {
		p.Draft["minimumQuantity"] = float64(1)
	}
	if input.PackSize == 0 {
		p.Draft["packSize"] = float64(1)
	}
	if input.ProductID <= 0 {
		p.require("product_id", "Für welches aktive Produkt gilt das Angebot?", nil)
	} else {
		rows, err := db.Query(ctx, `SELECT id,name,sku FROM proc_products WHERE id=$1 AND active=true`, input.ProductID)
		if err != nil {
			return p, err
		}
		if len(rows) != 1 {
			p.require("product_id", "Das Produkt existiert nicht oder ist archiviert.", nil)
		} else {
			p.RelatedRecords = append(p.RelatedRecords, rows[0])
		}
	}
	if input.SupplierID <= 0 {
		p.require("supplier_id", "Welcher aktive Lieferant bietet das Produkt an?", nil)
	} else {
		rows, err := db.Query(ctx, `SELECT id,name,code FROM proc_suppliers WHERE id=$1 AND active=true`, input.SupplierID)
		if err != nil {
			return p, err
		}
		if len(rows) != 1 {
			p.require("supplier_id", "Der Lieferant existiert nicht oder ist deaktiviert.", nil)
		} else {
			p.RelatedRecords = append(p.RelatedRecords, rows[0])
		}
	}
	if input.PriceCents == nil {
		p.require("price_cents", "Welcher Preis in Cent gilt für das Angebot?", nil)
	}
	validateOfferDraft(&p)
	if input.ProductID > 0 && input.SupplierID > 0 {
		duplicates, err := db.Query(ctx, `SELECT id AS offer_id,supplier_sku,price_cents,currency,active FROM proc_offers WHERE product_id=$1 AND supplier_id=$2 ORDER BY active DESC,id LIMIT 25`, input.ProductID, input.SupplierID)
		if err != nil {
			return p, err
		}
		if len(duplicates) > 0 {
			p.RelatedRecords = append(p.RelatedRecords, duplicates...)
			if !input.AllowDuplicate {
				p.require("duplicate_offer", "Ein Angebot dieses Lieferanten ist bereits vorhanden. Soll es aktualisiert statt neu angelegt werden?", duplicates)
			}
		}
	}
	p.finish()
	return p, nil
}

func prepareOfferUpdate(ctx context.Context, db *store.Store, input OfferUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.OfferID <= 0 {
		p.require("offer_id", "Welche gültige Angebots-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	if err := requireProcurementAdmin(ctx); err != nil {
		return p, err
	}
	rows, err := db.Query(ctx, `SELECT id,product_id,supplier_id,supplier_sku,price_cents,currency,minimum_quantity,pack_size,lead_days,purchase_url,valid_until,active,
		to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_offers WHERE id=$1`, input.OfferID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("offer_id", "Das Angebot wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	row := rows[0]
	version := rfc3339Value(row["updated_at"])
	p.Draft = map[string]any{
		"productId": row["product_id"], "supplierId": row["supplier_id"], "supplierSku": nullableText(row["supplier_sku"]),
		"priceCents": numericID(row["price_cents"]), "currency": nullableText(row["currency"]),
		"minimumQuantity": numericFloat(row["minimum_quantity"]), "packSize": numericFloat(row["pack_size"]),
		"leadDays": numericID(row["lead_days"]), "purchaseUrl": nullableText(row["purchase_url"]),
		"validUntil": row["valid_until"], "active": row["active"], "expectedUpdatedAt": version,
	}
	p.Current = cloneMap(p.Draft)
	if input.SupplierID != nil {
		p.Draft["supplierId"] = *input.SupplierID
	}
	if input.SupplierSKU != nil {
		p.Draft["supplierSku"] = strings.TrimSpace(*input.SupplierSKU)
	}
	if input.PriceCents != nil {
		p.Draft["priceCents"] = *input.PriceCents
	}
	if input.Currency != nil {
		p.Draft["currency"] = strings.ToUpper(strings.TrimSpace(*input.Currency))
	}
	if input.MinimumQuantity != nil {
		p.Draft["minimumQuantity"] = *input.MinimumQuantity
	}
	if input.PackSize != nil {
		p.Draft["packSize"] = *input.PackSize
	}
	if input.LeadDays != nil {
		p.Draft["leadDays"] = *input.LeadDays
	}
	if input.PurchaseURL != nil {
		p.Draft["purchaseUrl"] = strings.TrimSpace(*input.PurchaseURL)
	}
	if input.ValidUntil != nil {
		p.Draft["validUntil"] = strings.TrimSpace(*input.ValidUntil)
		if p.Draft["validUntil"] == "" {
			p.Draft["validUntil"] = nil
		}
	}
	if input.Active != nil {
		p.Draft["active"] = *input.Active
	}
	p.Diff = map[string]map[string]any{}
	for field, old := range p.Current {
		if field != "expectedUpdatedAt" && !reflect.DeepEqual(old, p.Draft[field]) {
			p.Diff[field] = map[string]any{"before": old, "after": p.Draft[field]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Angebotsfelder sollen geändert werden?", nil)
	}
	validateOfferDraft(&p)
	if supplierID := numericID(p.Draft["supplierId"]); supplierID > 0 {
		supplierQuery := `SELECT id,name,code,active FROM proc_suppliers WHERE id=$1`
		if p.Draft["active"] == true {
			supplierQuery += ` AND active=true`
		}
		suppliers, queryErr := db.Query(ctx, supplierQuery, supplierID)
		if queryErr != nil {
			return p, queryErr
		}
		if len(suppliers) != 1 {
			p.require("supplier_id", "Der Lieferant existiert nicht oder ist deaktiviert.", nil)
		} else {
			p.RelatedRecords = append(p.RelatedRecords, suppliers[0])
		}
	}
	if p.Draft["active"] == true {
		products, queryErr := db.Query(ctx, `SELECT id,name FROM proc_products WHERE id=$1 AND active=true`, p.Draft["productId"])
		if queryErr != nil {
			return p, queryErr
		}
		if len(products) != 1 {
			p.require("product_id", "Das Produkt ist archiviert; ein Angebot kann dafür nicht aktiviert werden.", nil)
		}
	}
	if input.SupplierID != nil {
		duplicates, queryErr := db.Query(ctx, `SELECT id AS offer_id,supplier_sku,price_cents,active FROM proc_offers WHERE product_id=$1 AND supplier_id=$2 AND id<>$3 ORDER BY active DESC,id LIMIT 25`, p.Draft["productId"], p.Draft["supplierId"], input.OfferID)
		if queryErr != nil {
			return p, queryErr
		}
		if len(duplicates) > 0 && !input.AllowDuplicate {
			p.RelatedRecords = append(p.RelatedRecords, duplicates...)
			p.require("duplicate_offer", "Ein anderes Angebot dieses Lieferanten besteht bereits. Ist die Umstellung wirklich gewollt?", duplicates)
		}
	}
	if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die exakte Version aus der aktuellen Vorschau muss übernommen werden.", version)
	} else if input.ExpectedUpdatedAt != "" && strings.TrimSpace(input.ExpectedUpdatedAt) != version {
		p.require("expected_updated_at", "Das Angebot wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", version)
	}
	p.finish()
	return p, nil
}

func validateOfferDraft(p *preparedMutation) {
	if price, ok := p.Draft["priceCents"]; ok && numericID(price) < 0 {
		p.require("price_cents", "Der Preis darf nicht negativ sein.", nil)
	}
	currency := fmt.Sprint(p.Draft["currency"])
	if len(currency) != 3 || strings.Trim(currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		p.require("currency", "Welcher dreibuchstabige ISO-Währungscode gilt?", nil)
	}
	for _, field := range []string{"minimumQuantity", "packSize"} {
		value := numericFloat(p.Draft[field])
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			p.require(field, "Menge und Packgröße müssen positive, endliche Zahlen sein.", nil)
		}
	}
	if numericID(p.Draft["leadDays"]) < 0 {
		p.require("lead_days", "Die Lieferzeit darf nicht negativ sein.", nil)
	}
	if value := fmt.Sprint(p.Draft["supplierSku"]); len([]rune(value)) > 120 {
		p.require("supplier_sku", "Die Lieferanten-SKU darf höchstens 120 Zeichen enthalten.", nil)
	}
	if value := fmt.Sprint(p.Draft["purchaseUrl"]); value != "" {
		parsed, err := url.ParseRequestURI(value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || len(value) > 2000 {
			p.require("purchase_url", "Welche gültige HTTP(S)-URL gehört zum Angebot?", nil)
		}
	}
	if value, ok := p.Draft["validUntil"].(string); ok && value != "" {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			p.require("valid_until", "Das Ablaufdatum muss als RFC3339-Zeitstempel angegeben werden.", nil)
		}
	}
}

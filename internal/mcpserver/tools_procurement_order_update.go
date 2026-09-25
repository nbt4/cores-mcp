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

type OrderDraftUpdateInput struct {
	MutationControl
	OrderID             int64                     `json:"order_id,omitempty"`
	SupplierID          *int64                    `json:"supplier_id,omitempty"`
	SupplierOrderNumber *string                   `json:"supplier_order_number,omitempty"`
	Currency            *string                   `json:"currency,omitempty"`
	OrderDate           *string                   `json:"order_date,omitempty" jsonschema:"RFC3339 timestamp; empty string clears."`
	ExpectedDelivery    *string                   `json:"expected_delivery,omitempty" jsonschema:"RFC3339 timestamp; empty string clears."`
	Notes               *string                   `json:"notes,omitempty"`
	Lines               *[]PurchaseOrderLineInput `json:"lines,omitempty" jsonschema:"Complete replacement line list; omit to preserve current lines."`
	ExpectedUpdatedAt   string                    `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update."`
	ConfirmUpdate       bool                      `json:"confirm_update,omitempty"`
}

func registerProcurementOrderDraftTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.orders.prepare_update", "Prepare purchase order draft update", "Load all order fields and lines, validate changes and related records, and show complete before/after diff and exact version.", func(ctx context.Context, input OrderDraftUpdateInput) (any, []Source, []string, error) {
		p, err := prepareOrderDraftUpdate(ctx, db, input)
		return p.response("draft"), orderDraftSources(input.OrderID, p), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.orders.update", "Update purchase order draft", "Replace confirmed fields and lines of one draft after exact version, full diff, target audit and durable idempotency.", func(ctx context.Context, input OrderDraftUpdateInput) (any, []Source, []string, error) {
		p, err := prepareOrderDraftUpdate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), orderDraftSources(input.OrderID, p), nil, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), orderDraftSources(input.OrderID, p), []string{"No data was changed. Show the complete draft diff and obtain explicit confirmation."}, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/orders/%d/draft", input.OrderID), http.MethodPut, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "purchase_order": result, "diff": p.Diff}, orderDraftSources(input.OrderID, p), nil, nil
	})
}

func orderDraftSources(id int64, p preparedMutation) []Source {
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(id)}}
	if supplierID := numericID(p.Draft["supplierId"]); supplierID > 0 {
		sources = append(sources, Source{Service: "procurementcore", Entity: "supplier", ID: fmt.Sprint(supplierID)})
	}
	return sources
}

func prepareOrderDraftUpdate(ctx context.Context, db *store.Store, input OrderDraftUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to update purchase order drafts")
	}
	if input.OrderID <= 0 {
		p.require("order_id", "Welcher Bestellentwurf soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT id,number,status,supplier_id,supplier_order_number,currency,order_date,expected_delivery,notes,total_cents,to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_purchase_orders WHERE id=$1`, input.OrderID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("order_id", "Bestellung wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	row := rows[0]
	if fmt.Sprint(row["status"]) != "draft" {
		p.require("status", "Nur Bestellentwürfe können vollständig geändert werden.", row["status"])
	}
	lineRows, err := db.Query(ctx, `SELECT id,product_id,description,quantity,unit,unit_price_cents,purchase_url,received_quantity FROM proc_purchase_order_lines WHERE purchase_order_id=$1 ORDER BY id`, input.OrderID)
	if err != nil {
		return p, err
	}
	lines := make([]map[string]any, 0, len(lineRows))
	for _, line := range lineRows {
		lines = append(lines, map[string]any{"productId": line["product_id"], "description": nullableText(line["description"]), "quantity": numericFloat(line["quantity"]), "unit": nullableText(line["unit"]), "unitPriceCents": numericID(line["unit_price_cents"]), "purchaseUrl": nullableText(line["purchase_url"])})
	}
	version := rfc3339Value(row["updated_at"])
	p.Current = map[string]any{"status": "draft", "supplierId": numericID(row["supplier_id"]), "supplierOrderNumber": nullableText(row["supplier_order_number"]), "currency": nullableText(row["currency"]), "orderDate": row["order_date"], "expectedDelivery": row["expected_delivery"], "notes": nullableText(row["notes"]), "lines": lines, "totalCents": numericID(row["total_cents"]), "expectedUpdatedAt": version}
	p.Draft = cloneMap(p.Current)
	if input.SupplierID != nil {
		p.Draft["supplierId"] = *input.SupplierID
	}
	for _, field := range []struct {
		key   string
		value *string
	}{{"supplierOrderNumber", input.SupplierOrderNumber}, {"currency", input.Currency}, {"notes", input.Notes}} {
		if field.value != nil {
			p.Draft[field.key] = strings.TrimSpace(*field.value)
		}
	}
	if input.Currency != nil {
		p.Draft["currency"] = strings.ToUpper(nullableText(p.Draft["currency"]))
	}
	for _, field := range []struct {
		key   string
		value *string
	}{{"orderDate", input.OrderDate}, {"expectedDelivery", input.ExpectedDelivery}} {
		if field.value == nil {
			continue
		}
		raw := strings.TrimSpace(*field.value)
		if raw == "" {
			p.Draft[field.key] = nil
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			p.require(toSnake(field.key), "Zeitpunkt im RFC3339-Format angeben.", nil)
		} else {
			p.Draft[field.key] = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	if input.Lines != nil {
		newLines := make([]map[string]any, 0, len(*input.Lines))
		for _, line := range *input.Lines {
			var productID any
			if line.ProductID != 0 {
				productID = line.ProductID
			}
			newLines = append(newLines, map[string]any{"productId": productID, "description": strings.TrimSpace(line.Description), "quantity": line.Quantity, "unit": strings.TrimSpace(line.Unit), "unitPriceCents": line.UnitPriceCents, "purchaseUrl": strings.TrimSpace(line.PurchaseURL)})
		}
		p.Draft["lines"] = newLines
	}
	if !containsString([]string{"EUR", "CHF", "USD", "GBP"}, nullableText(p.Draft["currency"])) {
		p.require("currency", "Unterstützte Währung EUR, CHF, USD oder GBP angeben.", nil)
	}
	if len([]rune(nullableText(p.Draft["supplierOrderNumber"]))) > 120 {
		p.require("supplier_order_number", "Lieferanten-Bestellnummer darf höchstens 120 Zeichen haben.", nil)
	}
	suppliers, err := db.Query(ctx, `SELECT id,name FROM proc_suppliers WHERE id=$1 AND active=true`, numericID(p.Draft["supplierId"]))
	if err != nil {
		return p, err
	}
	if len(suppliers) != 1 {
		p.require("supplier_id", "Aktiven Lieferanten auswählen.", nil)
	} else {
		p.RelatedRecords = append(p.RelatedRecords, suppliers[0])
	}
	if len(suppliers) == 1 && nullableText(p.Draft["supplierOrderNumber"]) != "" {
		duplicates, queryErr := db.Query(ctx, `SELECT id AS order_id,number FROM proc_purchase_orders WHERE id<>$1 AND supplier_id=$2 AND lower(supplier_order_number)=lower($3) LIMIT 20`, input.OrderID, numericID(p.Draft["supplierId"]), p.Draft["supplierOrderNumber"])
		if queryErr != nil {
			return p, queryErr
		}
		if len(duplicates) > 0 {
			p.RelatedRecords = append(p.RelatedRecords, duplicates...)
			p.require("duplicate_supplier_order_number", "Diese Lieferanten-Bestellnummer ist bereits vergeben.", duplicates)
		}
	}
	draftLines, _ := p.Draft["lines"].([]map[string]any)
	if len(draftLines) == 0 {
		p.require("lines", "Mindestens eine Bestellposition ist erforderlich.", nil)
	}
	if len(draftLines) > 100 {
		p.require("lines", "Höchstens 100 Positionen pro Bestellung.", nil)
	}
	var total float64
	for index, line := range draftLines {
		field := fmt.Sprintf("lines[%d]", index)
		if desc := nullableText(line["description"]); desc == "" || len([]rune(desc)) > 500 {
			p.require(field+".description", "Beschreibung mit höchstens 500 Zeichen angeben.", nil)
		}
		quantity := numericFloat(line["quantity"])
		if quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity > 1e9 {
			p.require(field+".quantity", "Endliche positive Menge unter einer Milliarde angeben.", nil)
		}
		price := numericID(line["unitPriceCents"])
		if price < 0 || price > 1e9 {
			p.require(field+".unit_price_cents", "Nicht-negativen Preis in Cent unter einer Milliarde angeben.", nil)
		}
		if unit := nullableText(line["unit"]); unit == "" {
			line["unit"] = "Stk."
		} else if len([]rune(unit)) > 30 {
			p.require(field+".unit", "Einheit ist zu lang.", nil)
		}
		if len(nullableText(line["purchaseUrl"])) > 2000 {
			p.require(field+".purchase_url", "Kauflink ist zu lang.", nil)
		}
		if productID := numericID(line["productId"]); productID != 0 {
			products, queryErr := db.Query(ctx, `SELECT id,name FROM proc_products WHERE id=$1 AND active=true`, productID)
			if queryErr != nil {
				return p, queryErr
			}
			if len(products) != 1 {
				p.require(field+".product_id", "Aktives Produkt auswählen oder die Produkt-ID weglassen.", nil)
			} else {
				p.RelatedRecords = append(p.RelatedRecords, products[0])
			}
		}
		total += quantity * float64(price)
	}
	if total > 9e18 {
		p.require("total_cents", "Gesamtwert ist zu groß.", nil)
	} else {
		p.Draft["totalCents"] = int64(total)
	}
	p.Diff = map[string]map[string]any{}
	for _, key := range []string{"supplierId", "supplierOrderNumber", "currency", "orderDate", "expectedDelivery", "notes", "lines", "totalCents"} {
		if !reflect.DeepEqual(p.Current[key], p.Draft[key]) {
			p.Diff[key] = map[string]any{"before": p.Current[key], "after": p.Draft[key]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Bestellfelder oder Positionen sollen geändert werden?", nil)
	}
	if input.ConfirmUpdate && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Exakte Version aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Bestellung wurde seit der Vorschau geändert.", version)
	}
	p.finish()
	return p, nil
}

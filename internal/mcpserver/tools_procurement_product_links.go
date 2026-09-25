package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type ProductLinkInput struct {
	MutationControl
	ProcurementProductID int64  `json:"procurement_product_id,omitempty"`
	WarehouseProductID   int64  `json:"warehouse_product_id,omitempty"`
	AllowNameMismatch    bool   `json:"allow_name_mismatch,omitempty" jsonschema:"Set only after explicitly reviewing different product names and confirming they represent the same item."`
	ExpectedUpdatedAt    string `json:"expected_updated_at,omitempty" jsonschema:"Exact product or existing link version from prepare_link."`
	ConfirmLink          bool   `json:"confirm_link,omitempty"`
	ConfirmationText     string `json:"confirmation_text,omitempty" jsonschema:"Record-bound phrase from prepare_link."`
}

func registerProcurementProductLinkTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.product_links.prepare_link", "Prepare cross-Core product link", "Check both active products, existing links, name match, open orders and receipts; show exact link diff, version and confirmation phrase.", func(ctx context.Context, input ProductLinkInput) (any, []Source, []string, error) {
		p, err := prepareProcurementProductLink(ctx, db, input)
		return p.response("draft"), productLinkSources(input, p), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.product_links.link", "Link cross-Core products", "Create or replace one confirmed Procurement-to-Warehouse product identity link with exact version, target audit, idempotency and elevated confirmation.", func(ctx context.Context, input ProductLinkInput) (any, []Source, []string, error) {
		p, err := prepareProcurementProductLink(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), productLinkSources(input, p), nil, err
		}
		if !input.ConfirmLink {
			return p.response("confirmation_required"), productLinkSources(input, p), []string{"No data was changed. Show both product identities and the full diff before confirmation."}, nil
		}
		if strings.TrimSpace(input.ConfirmationText) != productLinkPhrase(input) {
			return p.response("elevated_confirmation_required"), productLinkSources(input, p), []string{"No data was changed. Type the phrase shown by prepare_link."}, nil
		}
		payload := map[string]any{"warehouseProductId": input.WarehouseProductID, "expectedUpdatedAt": input.ExpectedUpdatedAt}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/products/%d/warehouse-link", input.ProcurementProductID), http.MethodPost, payload, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "linked", "product_link": result, "diff": p.Diff}, productLinkSources(input, p), p.Warnings, nil
	})
}

func productLinkPhrase(input ProductLinkInput) string {
	if input.ProcurementProductID <= 0 || input.WarehouseProductID <= 0 {
		return ""
	}
	return fmt.Sprintf("LINK PROCUREMENT %d WAREHOUSE %d", input.ProcurementProductID, input.WarehouseProductID)
}

func productLinkSources(input ProductLinkInput, p preparedMutation) []Source {
	sources := []Source{{Service: "procurementcore", Entity: "product", ID: fmt.Sprint(input.ProcurementProductID)}}
	if input.WarehouseProductID > 0 {
		sources = append(sources, Source{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(input.WarehouseProductID)})
	}
	if id := numericID(p.Current["link_id"]); id > 0 {
		sources = append(sources, Source{Service: "cores", Entity: "product_link", ID: fmt.Sprint(id)})
	}
	return sources
}

func prepareProcurementProductLink(ctx context.Context, db *store.Store, input ProductLinkInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to link products")
	}
	if input.ProcurementProductID <= 0 {
		p.require("procurement_product_id", "Welches Beschaffungsprodukt soll verknüpft werden?", nil)
	}
	if input.WarehouseProductID <= 0 {
		p.require("warehouse_product_id", "Welches Warehouse-Produkt ist derselbe physische Artikel?", nil)
	}
	if input.ProcurementProductID <= 0 || input.WarehouseProductID <= 0 {
		p.finish()
		return p, nil
	}
	products, err := db.Query(ctx, `SELECT id,sku,name,active,to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM proc_products WHERE id=$1`, input.ProcurementProductID)
	if err != nil {
		return p, err
	}
	if len(products) != 1 || products[0]["active"] != true {
		p.require("procurement_product_id", "Beschaffungsprodukt fehlt oder ist archiviert.", nil)
		p.finish()
		return p, nil
	}
	warehouse, err := db.Query(ctx, `SELECT productID AS warehouse_product_id,product_code,name,tracking_mode,lifecycle_status FROM products WHERE productID=$1`, input.WarehouseProductID)
	if err != nil {
		return p, err
	}
	if len(warehouse) != 1 || warehouse[0]["lifecycle_status"] != "active" {
		p.require("warehouse_product_id", "Warehouse-Produkt fehlt oder ist archiviert.", nil)
		p.finish()
		return p, nil
	}
	p.RelatedRecords = append(p.RelatedRecords, products[0], warehouse[0])
	currentLinks, err := db.Query(ctx, `SELECT id AS link_id,warehouse_product_id,to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM core_product_links WHERE procurement_product_id=$1`, input.ProcurementProductID)
	if err != nil {
		return p, err
	}
	version := rfc3339Value(products[0]["updated_at"])
	var previous any
	if len(currentLinks) > 0 {
		p.Current = currentLinks[0]
		previous = currentLinks[0]["warehouse_product_id"]
		version = rfc3339Value(currentLinks[0]["updated_at"])
	}
	if numericID(previous) == input.WarehouseProductID {
		p.require("changes", "Diese Produkte sind bereits miteinander verknüpft.", nil)
	}
	used, err := db.Query(ctx, `SELECT id AS link_id,procurement_product_id FROM core_product_links WHERE warehouse_product_id=$1 AND procurement_product_id<>$2`, input.WarehouseProductID, input.ProcurementProductID)
	if err != nil {
		return p, err
	}
	if len(used) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, used...)
		p.require("warehouse_product_id", "Warehouse-Produkt ist bereits mit einem anderen Beschaffungsprodukt verknüpft.", used)
	}
	procName := normalizeIdentity(nullableText(products[0]["name"]))
	warehouseName := normalizeIdentity(nullableText(warehouse[0]["name"]))
	if procName != "" && warehouseName != "" && !strings.Contains(procName, warehouseName) && !strings.Contains(warehouseName, procName) {
		p.Warnings = append(p.Warnings, "Product names differ. Verify SKU, model and physical identity before linking.")
		if !input.AllowNameMismatch {
			p.require("allow_name_mismatch", "Die Produktnamen unterscheiden sich. Sind dies nach Prüfung wirklich dieselben Artikel?", nil)
		}
	}
	if len(currentLinks) > 0 && numericID(previous) != input.WarehouseProductID {
		receipts, queryErr := db.Query(ctx, `SELECT r.id AS receipt_id,po.id AS order_id FROM proc_receipts r JOIN proc_purchase_order_lines pol ON pol.id=r.purchase_order_line_id JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id WHERE pol.product_id=$1 ORDER BY r.id LIMIT 25`, input.ProcurementProductID)
		if queryErr != nil {
			return p, queryErr
		}
		orders, queryErr := db.Query(ctx, `SELECT po.id AS order_id,po.number,po.status FROM proc_purchase_order_lines pol JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id WHERE pol.product_id=$1 AND po.status IN ('draft','sent','confirmed','partially_received') ORDER BY po.id LIMIT 25`, input.ProcurementProductID)
		if queryErr != nil {
			return p, queryErr
		}
		p.RelatedRecords = append(p.RelatedRecords, receipts...)
		p.RelatedRecords = append(p.RelatedRecords, orders...)
		if len(receipts)+len(orders) > 0 {
			p.require("active_references", "Wareneingänge oder offene Bestellungen verhindern die Neuverknüpfung.", p.RelatedRecords)
		}
	}
	if input.ConfirmLink && input.ExpectedUpdatedAt == "" {
		p.require("expected_updated_at", "Exakte Version aus der Vorschau übernehmen.", version)
	} else if input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Produktverknüpfung wurde seit der Vorschau geändert.", version)
	}
	p.Draft = map[string]any{"procurement_product_id": input.ProcurementProductID, "warehouse_product_id": input.WarehouseProductID, "expected_updated_at": version, "confirmation_text_required": productLinkPhrase(input)}
	p.Diff = map[string]map[string]any{"warehouse_product_id": {"before": previous, "after": input.WarehouseProductID}}
	p.finish()
	return p, nil
}

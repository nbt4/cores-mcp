package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nbt4/cores-mcp/internal/config"
)

func invokeProcurementGoodsReceipt(ctx context.Context, cfg config.Config, input PurchaseOrderReceiptInput, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	sources := []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(input.OrderID)}, {Service: "procurementcore", Entity: "purchase_order_line", ID: fmt.Sprint(input.LineID)}}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, sources, nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, sources, nil, err
	}
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || input.DryRun || !input.ConfirmReceipt
	result := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/orders/receive", http.MethodPost, body, &result)
	if current, ok := result["current"].(map[string]any); ok {
		for _, target := range []struct{ key, service, entity, id string }{
			{"supplier", "procurementcore", "supplier", "id"}, {"procurement_product", "procurementcore", "product", "id"},
			{"warehouse_link", "cores", "product_link", "id"}, {"warehouse_product", "warehousecore", "product", "productid"},
			{"target_zone", "warehousecore", "storage_zone", "zone_id"}, {"lines", "procurementcore", "purchase_order_line", "id"},
			{"stock_locations", "warehousecore", "product_location", "id"},
		} {
			if rows, ok := current[target.key].([]any); ok {
				for _, value := range rows {
					if row, ok := value.(map[string]any); ok && row[target.id] != nil {
						sources = append(sources, Source{Service: target.service, Entity: target.entity, ID: fmt.Sprint(row[target.id])})
					}
				}
			}
		}
	}
	if receipt, ok := result["receipt"].(map[string]any); ok {
		if id := receipt["id"]; id != nil {
			sources = append(sources, Source{Service: "procurementcore", Entity: "goods_receipt", ID: fmt.Sprint(id)})
		}
		if id := receipt["warehouseProductId"]; id != nil {
			sources = append(sources, Source{Service: "warehousecore", Entity: "product", ID: fmt.Sprint(id)})
		}
		if id := receipt["putawayTaskId"]; id != nil {
			sources = append(sources, Source{Service: "warehousecore", Entity: "warehouse_task", ID: fmt.Sprint(id)})
		}
		if ids, ok := receipt["createdDeviceIds"].([]any); ok {
			for _, id := range ids {
				sources = append(sources, Source{Service: "warehousecore", Entity: "device", ID: fmt.Sprint(id)})
			}
		}
	}
	return result, sources, append(untrustedTextWarning(), "Goods receipt requires explicitly granted procurement receive scope and current owning-Core administrator. Exact order/version/context and quantity-bound confirmation freeze stock mapping, serials and destination. Order quantities, stock/devices, putaway task/event, audits/activity and durable receipt commit together. At most 1000 devices; no prices or external messages change. Replays recheck current owner rights before returning saved results."), err
}

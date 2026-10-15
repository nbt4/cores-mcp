package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

type RentalExternalEquipmentInput struct {
	MutationControl
	Name             string   `json:"name,omitempty" jsonschema:"External rental catalog name, required, maximum 255 characters."`
	Category         string   `json:"category,omitempty" jsonschema:"Required rental category label, maximum 100 characters."`
	Supplier         string   `json:"supplier,omitempty" jsonschema:"Supplier business name, maximum 255 characters; required unless supplier_id resolves it."`
	SupplierID       int64    `json:"supplier_id,omitempty" jsonschema:"Optional exact active Rental supplier customer ID. A supplied name must match this identity."`
	RentalPrice      *float64 `json:"rental_price,omitempty" jsonschema:"Explicit supplier rental price in EUR, 0–99999999.99, maximum two decimals. Zero is allowed; omission is never zero."`
	CustomerPrice    *float64 `json:"customer_price,omitempty" jsonschema:"Explicit customer rental price in EUR, 0–99999999.99, maximum two decimals. Uses the existing job pricing rules when assigned."`
	Description      string   `json:"description,omitempty" jsonschema:"Optional business description, maximum 10000 characters."`
	Notes            string   `json:"notes,omitempty" jsonschema:"Optional business notes, maximum 10000 characters."`
	AllowSimilar     bool     `json:"allow_similar,omitempty" jsonschema:"Only after explicitly reviewing similar existing items. Exact name and supplier duplicates are always blocked, including inactive items."`
	ExpectedContext  string   `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 from final owner preview, binding draft, supplier identity and duplicate candidates."`
	ConfirmationText string   `json:"confirmation_text,omitempty" jsonschema:"Exact required_confirmation_text from the final preview."`
	ConfirmChange    bool     `json:"confirm_change,omitempty" jsonschema:"Only after presenting complete final draft and obtaining explicit business confirmation."`
}

func invokeRentalExternalEquipment(ctx context.Context, cfg config.Config, in RentalExternalEquipmentInput, preview bool) (any, []Source, []string, error) {
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
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || in.DryRun || !in.ConfirmChange
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/external-equipment/create", http.MethodPost, body, &out)
	id := ""
	if record, ok := out["equipment"].(map[string]any); ok && record["equipment_id"] != nil {
		id = fmt.Sprint(record["equipment_id"])
	}
	sources := []Source{{Service: "rentalcore", Entity: "rental_equipment", ID: id}}
	if in.SupplierID != 0 {
		sources = append(sources, Source{Service: "rentalcore", Entity: "customer", ID: fmt.Sprint(in.SupplierID)})
	}
	return out, sources, []string{"Creates one external rental catalog item with separate supplier and customer prices. Requires current Rental administrator/create rights and separately selected rental financial access, exact preview context, draft-bound confirmation and durable idempotency. Exact name/supplier duplicates, including inactive items, block creation. Catalog record, audit and replay receipt commit atomically. No warehouse product, stock movement, procurement action or job assignment is created."}, err
}

func registerRentalExternalEquipmentWrites(server *mcp.Server, cfg config.Config) {
	addWritePreparationTool(server, "rental.external_equipment.prepare_create", "Prepare external rental catalog item", "Review name, category, supplier, both explicit prices and duplicate candidates before creating a standalone external rental catalog item. Does not change data.", func(ctx context.Context, in RentalExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalExternalEquipment(ctx, cfg, in, true)
	})
	addCreateTool(server, "rental.external_equipment.create", "Create external rental catalog item", "Create one externally hired catalog item through RentalCore after final preview and explicit draft-bound confirmation. Requires separate create and financial access, validates supplier and duplicates, and never creates owned stock or assigns a job.", func(ctx context.Context, in RentalExternalEquipmentInput) (any, []Source, []string, error) {
		return invokeRentalExternalEquipment(ctx, cfg, in, false)
	})
}

package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseManufacturerCreateInput struct {
	MutationControl
	Name            string `json:"name,omitempty" jsonschema:"Manufacturer name, 1-255 characters; check resolve results before creating."`
	Website         string `json:"website,omitempty" jsonschema:"Optional HTTP(S) website. A bare hostname is normalized to https://."`
	AllowSimilar    bool   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing manufacturers."`
	ConfirmCreation bool   `json:"confirm_creation,omitempty" jsonschema:"Confirm the final manufacturer draft after preview."`
}

type WarehouseBrandCreateInput struct {
	MutationControl
	Name            string `json:"name,omitempty" jsonschema:"Brand or product-line name, 1-255 characters."`
	ManufacturerID  int64  `json:"manufacturer_id,omitempty" jsonschema:"Existing manufacturer ID, for example returned by warehouse.manufacturers.create."`
	AllowSimilar    bool   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing brands."`
	ConfirmCreation bool   `json:"confirm_creation,omitempty" jsonschema:"Confirm the final brand draft after preview."`
}

func registerWarehouseMasterCreateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.manufacturers.prepare_create", "Prepare warehouse manufacturer", "Validate a standalone manufacturer name and website, and show exact or similar existing manufacturers before creation.", func(ctx context.Context, input WarehouseManufacturerCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseManufacturerCreate(ctx, db, input)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "manufacturer"}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.manufacturers.create", "Create warehouse manufacturer", "Create one standalone WarehouseCore manufacturer after duplicate review and explicit confirmation. WarehouseCore commits its audit and durable idempotency receipt with the record.", func(ctx context.Context, input WarehouseManufacturerCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseManufacturerCreate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "manufacturer"}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "manufacturer"}}, append(p.Warnings, "No data was changed."), nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/manufacturers", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "manufacturer": created}, []Source{{Service: "warehousecore", Entity: "manufacturer", ID: fmt.Sprint(created["manufacturer_id"])}}, p.Warnings, nil
	})
	addWritePreparationTool(server, "warehouse.brands.prepare_create", "Prepare warehouse brand", "Check the existing manufacturer, brand name, exact duplicates and similar product lines before standalone creation.", func(ctx context.Context, input WarehouseBrandCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseBrandCreate(ctx, db, input)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "manufacturer", ID: fmt.Sprint(input.ManufacturerID)}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.brands.create", "Create warehouse brand", "Create one brand for an existing WarehouseCore manufacturer after duplicate review and explicit confirmation. WarehouseCore commits its audit and durable idempotency receipt with the record.", func(ctx context.Context, input WarehouseBrandCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseBrandCreate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "manufacturer", ID: fmt.Sprint(input.ManufacturerID)}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "manufacturer", ID: fmt.Sprint(input.ManufacturerID)}}, append(p.Warnings, "No data was changed."), nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/brands", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "brand": created}, []Source{{Service: "warehousecore", Entity: "manufacturer", ID: fmt.Sprint(input.ManufacturerID)}, {Service: "warehousecore", Entity: "brand", ID: fmt.Sprint(created["brand_id"])}}, p.Warnings, nil
	})
}

func requireWarehouseMasterAdmin(ctx context.Context) error {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return fmt.Errorf("Warehouse administrator permission is required to create manufacturer or brand")
	}
	return nil
}

func prepareWarehouseManufacturerCreate(ctx context.Context, db *store.Store, input WarehouseManufacturerCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 255 {
		p.require("name", "Welcher Herstellername mit 1-255 Zeichen soll angelegt werden?", nil)
	}
	website, err := warehouseManufacturerWebsite(input.Website)
	if err != nil {
		p.require("website", "Welche gültige HTTP(S)-Website mit höchstens 255 Zeichen gilt?", nil)
	}
	p.Draft = map[string]any{"name": name, "website": website}
	if name == "" {
		p.finish()
		return p, nil
	}
	duplicates, err := db.Query(ctx, `SELECT manufacturerid AS id,name,website FROM manufacturer WHERE lower(trim(name))=lower($1) LIMIT 10`, name)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_manufacturer", "Hersteller existiert bereits; vorhandene ID verwenden.", duplicates)
	}
	candidates, err := warehouseMasterCandidates(ctx, db, "manufacturer", name, 20)
	if err != nil {
		return p, err
	}
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" {
			continue
		}
		p.RelatedRecords = append(p.RelatedRecords, candidate)
	}
	if len(p.RelatedRecords) > len(duplicates) && !input.AllowSimilar {
		p.require("similar_manufacturer_review", "Ähnliche Hersteller prüfen und nur nach ausdrücklicher Freigabe allow_similar setzen.", p.RelatedRecords)
	}
	p.Diff = map[string]map[string]any{"name": {"before": nil, "after": name}, "website": {"before": nil, "after": website}}
	p.finish()
	return p, nil
}

func prepareWarehouseBrandCreate(ctx context.Context, db *store.Store, input WarehouseBrandCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 255 {
		p.require("name", "Welcher Markenname mit 1-255 Zeichen soll angelegt werden?", nil)
	}
	if input.ManufacturerID <= 0 {
		p.require("manufacturer_id", "Welche vorhandene Hersteller-ID gehört zu dieser Marke?", nil)
	}
	p.Draft = map[string]any{"name": name, "manufacturer_id": input.ManufacturerID}
	if name == "" || input.ManufacturerID <= 0 {
		p.finish()
		return p, nil
	}
	manufacturer, err := db.Query(ctx, `SELECT manufacturerid AS id,name FROM manufacturer WHERE manufacturerid=$1`, input.ManufacturerID)
	if err != nil {
		return p, err
	}
	if len(manufacturer) != 1 {
		p.require("manufacturer_id", "Hersteller fehlt; zuerst anlegen oder vorhandene ID wählen.", nil)
		p.finish()
		return p, nil
	}
	p.RelatedRecords = append(p.RelatedRecords, manufacturer[0])
	duplicates, err := db.Query(ctx, `SELECT brandid AS id,name,manufacturerid AS manufacturer_id FROM brands WHERE manufacturerid=$1 AND lower(trim(name))=lower($2) LIMIT 10`, input.ManufacturerID, name)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_brand", "Marke existiert bereits für diesen Hersteller; vorhandene ID verwenden.", duplicates)
	}
	candidates, err := warehouseMasterCandidates(ctx, db, "brand", name, 20)
	if err != nil {
		return p, err
	}
	similar := 0
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" && numericID(candidate["manufacturer_id"]) == input.ManufacturerID {
			continue
		}
		p.RelatedRecords = append(p.RelatedRecords, candidate)
		similar++
	}
	if similar > 0 && !input.AllowSimilar {
		p.require("similar_brand_review", "Ähnliche Marken prüfen und nur nach ausdrücklicher Freigabe allow_similar setzen.", candidates)
	}
	p.Diff = map[string]map[string]any{"name": {"before": nil, "after": name}, "manufacturer_id": {"before": nil, "after": input.ManufacturerID}}
	p.finish()
	return p, nil
}

func warehouseManufacturerWebsite(raw string) (*string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || len([]rune(value)) > 255 {
		return nil, fmt.Errorf("invalid website")
	}
	return &value, nil
}

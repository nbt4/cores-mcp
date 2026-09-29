package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseLocationCreateInput struct {
	MutationControl
	Code                   string   `json:"code,omitempty" jsonschema:"Unique location code, 1-50 characters."`
	Name                   string   `json:"name,omitempty" jsonschema:"Location name, 1-100 characters."`
	Barcode                string   `json:"barcode,omitempty" jsonschema:"Optional scan code; defaults to LOC- plus code."`
	Type                   string   `json:"type,omitempty" jsonschema:"shelf, rack, case, vehicle, stage, warehouse or other; defaults from location_kind."`
	LocationKind           string   `json:"location_kind,omitempty" jsonschema:"Site, rack, bin, level, vehicle or area; defaults to area."`
	ProcessRole            string   `json:"process_role,omitempty" jsonschema:"storage, receiving, return, inspection, quarantine, repair, charging, picking, staging, shipping, transport or unknown."`
	Description            string   `json:"description,omitempty"`
	ParentZoneID           int64    `json:"parent_zone_id,omitempty" jsonschema:"Existing active parent location ID."`
	Capacity               *float64 `json:"capacity,omitempty" jsonschema:"Optional positive item capacity."`
	IsStorable             *bool    `json:"is_storable,omitempty" jsonschema:"Defaults to true; explicitly set false for nonstorage areas."`
	PickSequence           *int     `json:"pick_sequence,omitempty"`
	MaxWeightKg            *float64 `json:"max_weight_kg,omitempty"`
	MaxVolumeM3            *float64 `json:"max_volume_m3,omitempty"`
	InventoryFrequencyDays *int     `json:"inventory_frequency_days,omitempty"`
	AllowSimilar           bool     `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar existing locations."`
	ConfirmCreation        bool     `json:"confirm_creation,omitempty" jsonschema:"Set only after presenting the final location draft."`
}

func registerWarehouseLocationCreateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "warehouse.locations.prepare_create", "Prepare warehouse location", "Validate all location fields, parent, code, barcode, exact duplicates and similar names before creating a storage location.", func(ctx context.Context, input WarehouseLocationCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseLocationCreate(ctx, db, input)
		return p.response("draft"), []Source{{Service: "warehousecore", Entity: "storage_zone"}}, p.Warnings, err
	})
	addCreateTool(server, "warehouse.locations.create", "Create warehouse location", "Create one storage location after duplicate review and explicit confirmation. WarehouseCore commits the location, audit and durable idempotency receipt together.", func(ctx context.Context, input WarehouseLocationCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseLocationCreate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), []Source{{Service: "warehousecore", Entity: "storage_zone"}}, p.Warnings, err
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), []Source{{Service: "warehousecore", Entity: "storage_zone"}}, append(p.Warnings, "No data was changed."), nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/warehouse/locations", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "location": created}, []Source{{Service: "warehousecore", Entity: "storage_zone", ID: fmt.Sprint(created["zone_id"])}}, nil, nil
	})
}

func prepareWarehouseLocationCreate(ctx context.Context, db *store.Store, input WarehouseLocationCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	name := strings.TrimSpace(input.Name)
	barcode := strings.TrimSpace(input.Barcode)
	if code == "" || len(code) > 50 {
		p.require("code", "Welcher eindeutige Lagerplatzcode mit höchstens 50 Zeichen gilt?", nil)
	}
	if name == "" || len([]rune(name)) > 100 {
		p.require("name", "Welcher Lagerplatzname mit höchstens 100 Zeichen gilt?", nil)
	}
	if barcode == "" {
		barcode = "LOC-" + code
	}
	if len(barcode) > 255 {
		p.require("barcode", "Der Scan-Code darf höchstens 255 Zeichen haben.", nil)
	}
	kind := strings.TrimSpace(input.LocationKind)
	if kind == "" {
		kind = "area"
	}
	typ := strings.TrimSpace(input.Type)
	if typ == "" {
		switch kind {
		case "site":
			typ = "warehouse"
		case "rack":
			typ = "rack"
		case "bin", "level":
			typ = "shelf"
		case "vehicle":
			typ = "vehicle"
		default:
			typ = "other"
		}
	}
	if !oneOf(typ, "shelf", "rack", "case", "vehicle", "stage", "warehouse", "other") {
		p.require("type", "Welcher gültige Lagerplatztyp gilt?", nil)
	}
	if !oneOf(kind, "site", "rack", "bin", "level", "vehicle", "area") {
		p.require("location_kind", "Welche gültige Lagerplatzart gilt?", nil)
	}
	role := strings.TrimSpace(input.ProcessRole)
	if role == "" {
		role = "storage"
	}
	if !oneOf(role, "storage", "receiving", "return", "inspection", "quarantine", "repair", "charging", "picking", "staging", "shipping", "transport", "unknown") {
		p.require("process_role", "Welche gültige Prozessrolle gilt?", nil)
	}
	if input.Capacity != nil && *input.Capacity <= 0 {
		p.require("capacity", "Kapazität muss größer als null sein.", nil)
	}
	if input.MaxWeightKg != nil && *input.MaxWeightKg <= 0 {
		p.require("max_weight_kg", "Maximalgewicht muss größer als null sein.", nil)
	}
	if input.MaxVolumeM3 != nil && *input.MaxVolumeM3 <= 0 {
		p.require("max_volume_m3", "Maximalvolumen muss größer als null sein.", nil)
	}
	if input.InventoryFrequencyDays != nil && *input.InventoryFrequencyDays < 0 {
		p.require("inventory_frequency_days", "Inventurintervall darf nicht negativ sein.", nil)
	}
	if input.ParentZoneID < 0 {
		p.require("parent_zone_id", "Eltern-ID muss positiv sein.", nil)
	}
	storable := true
	if input.IsStorable != nil {
		storable = *input.IsStorable
	}
	var parent any
	if input.ParentZoneID > 0 {
		parent = input.ParentZoneID
	}
	p.Draft = map[string]any{"code": code, "name": name, "barcode": barcode, "type": typ, "location_kind": kind,
		"process_role": role, "operational_status": "available", "description": strings.TrimSpace(input.Description),
		"parent_zone_id": parent, "capacity": input.Capacity, "capacity_mode": "item_count", "is_storable": storable,
		"pick_sequence": input.PickSequence, "max_weight_kg": input.MaxWeightKg, "max_volume_m3": input.MaxVolumeM3,
		"inventory_frequency_days": input.InventoryFrequencyDays}
	if len(p.Missing) > 0 {
		p.finish()
		return p, nil
	}
	if parent != nil {
		rows, err := db.Query(ctx, `SELECT zone_id AS id,code,name,operational_status FROM storage_zones WHERE zone_id=$1 AND is_active=true`, parent)
		if err != nil {
			return p, err
		}
		if len(rows) != 1 {
			p.require("parent_zone_id", "Übergeordneter Lagerplatz fehlt oder ist archiviert.", nil)
			p.finish()
			return p, nil
		}
		p.RelatedRecords = append(p.RelatedRecords, rows[0])
	}
	duplicates, err := db.Query(ctx, `SELECT zone_id AS id,code,name,barcode FROM storage_zones WHERE lower(trim(code))=lower($1) OR lower(trim(barcode))=lower($2) OR (lower(trim(name))=lower($3) AND parent_zone_id IS NOT DISTINCT FROM $4) LIMIT 10`, code, barcode, name, parent)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_location", "Lagerplatzcode, Scan-Code oder Name unter demselben Elternknoten ist bereits vorhanden.", duplicates)
	}
	candidates, err := warehouseMasterCandidates(ctx, db, "zone", name, 20)
	if err != nil {
		return p, err
	}
	similar := false
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" {
			continue
		}
		p.RelatedRecords = append(p.RelatedRecords, candidate)
		similar = true
	}
	if similar && !input.AllowSimilar {
		p.require("similar_location_review", "Ähnliche Lagerplätze prüfen und nur nach ausdrücklicher Freigabe allow_similar setzen.", candidates)
	}
	p.Diff = make(map[string]map[string]any, len(p.Draft))
	for key, value := range p.Draft {
		p.Diff[key] = map[string]any{"before": nil, "after": value}
	}
	p.finish()
	return p, nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

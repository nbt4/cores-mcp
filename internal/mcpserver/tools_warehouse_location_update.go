package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseLocationUpdateInput struct {
	MutationControl
	ZoneID                 int64    `json:"zone_id,omitempty" jsonschema:"Exact WarehouseCore location ID."`
	Code                   *string  `json:"code,omitempty" jsonschema:"Unique replacement code, 1-50 characters."`
	Name                   *string  `json:"name,omitempty" jsonschema:"Replacement name, 1-100 characters; unique within its parent."`
	Barcode                *string  `json:"barcode,omitempty" jsonschema:"Unique scan code, at most 255 characters; empty regenerates LOC- plus code."`
	Type                   *string  `json:"type,omitempty" jsonschema:"shelf, rack, case, vehicle, stage, warehouse or other."`
	LocationKind           *string  `json:"location_kind,omitempty" jsonschema:"site, rack, bin, level, vehicle or area."`
	ProcessRole            *string  `json:"process_role,omitempty" jsonschema:"storage, receiving, return, inspection, quarantine, repair, charging, picking, staging, shipping, transport or unknown."`
	Description            *string  `json:"description,omitempty" jsonschema:"Replacement description; empty clears it."`
	ParentZoneID           *int64   `json:"parent_zone_id,omitempty" jsonschema:"Active parent ID; hierarchy cycles are forbidden."`
	Capacity               *float64 `json:"capacity,omitempty" jsonschema:"Positive whole item capacity, at most 2147483647; cannot be below occupancy."`
	IsStorable             *bool    `json:"is_storable,omitempty" jsonschema:"Set false only when the location has no inventory."`
	PickSequence           *int     `json:"pick_sequence,omitempty" jsonschema:"Signed 32-bit picking order."`
	MaxWeightKg            *float64 `json:"max_weight_kg,omitempty" jsonschema:"Positive limit in kilograms, at most 999999999.999."`
	MaxVolumeM3            *float64 `json:"max_volume_m3,omitempty" jsonschema:"Positive limit in cubic meters, at most 999999.999999."`
	InventoryFrequencyDays *int     `json:"inventory_frequency_days,omitempty" jsonschema:"Nonnegative signed 32-bit count interval in days; zero disables scheduling."`
	ClearFields            []string `json:"clear_fields,omitempty" jsonschema:"Nullable fields to clear: description, parent_zone_id, capacity, pick_sequence, max_weight_kg, max_volume_m3, inventory_frequency_days. Do not also supply that field."`
	AllowSimilar           bool     `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar locations for a changed name."`
	ExpectedUpdatedAt      string   `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate          bool     `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the full diff and receiving explicit confirmation."`
}

var warehouseLocationEditableFields = []string{"code", "barcode", "name", "type", "location_kind", "process_role", "description", "parent_zone_id", "capacity", "capacity_mode", "is_storable", "pick_sequence", "max_weight_kg", "max_volume_m3", "inventory_frequency_days", "operational_status"}

func registerWarehouseLocationUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	sources := func(id int64, p preparedMutation) []Source {
		return append([]Source{{Service: "warehousecore", Entity: "storage_zone", ID: fmt.Sprint(id)}}, sourcesFor("warehousecore", "storage_zone", p.RelatedRecords)...)
	}
	addWritePreparationTool(server, "warehouse.locations.prepare_update", "Prepare warehouse location update", "Load all editable location fields for an administrator and preview the full diff, exact version, identity conflicts, parent hierarchy and occupancy without changing data.", func(ctx context.Context, input WarehouseLocationUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseLocationUpdate(ctx, db, input)
		return p.response("draft"), sources(input.ZoneID, p), p.Warnings, err
	})
	addUpdateTool(server, "warehouse.locations.update", "Update warehouse location", "Update one active location after a full preview, exact version and explicit confirmation. WarehouseCore commits the edit, audit and durable replay receipt atomically and rechecks hierarchy and inventory limits.", func(ctx context.Context, input WarehouseLocationUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehouseLocationUpdate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), sources(input.ZoneID, p), p.Warnings, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), sources(input.ZoneID, p), append(p.Warnings, "No data was changed. Present the complete diff and obtain explicit confirmation."), nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/warehouse/locations/%d", input.ZoneID), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "location": updated, "diff": p.Diff}, sources(input.ZoneID, p), p.Warnings, nil
	})
}

func prepareWarehouseLocationUpdate(ctx context.Context, db *store.Store, input WarehouseLocationUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	if input.ZoneID <= 0 {
		p.require("zone_id", "Welche gültige Lagerplatz-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT zone_id,code,barcode,name,type::text,location_kind,process_role,description,parent_zone_id,capacity,capacity_mode,is_storable,pick_sequence,max_weight_kg,max_volume_m3,inventory_frequency_days,operational_status,is_active,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM storage_zones WHERE zone_id=$1`, input.ZoneID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("zone_id", "Lagerplatz wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	version := fmt.Sprint(p.Current["updated_at"])
	for _, key := range warehouseLocationEditableFields {
		p.Draft[key] = p.Current[key]
	}
	for _, key := range []string{"capacity", "max_weight_kg", "max_volume_m3"} {
		if p.Draft[key] != nil {
			p.Draft[key] = numericFloat(p.Draft[key])
		}
	}
	before := cloneMap(p.Draft)
	replacements := map[string]any{"code": input.Code, "barcode": input.Barcode, "name": input.Name, "type": input.Type, "location_kind": input.LocationKind, "process_role": input.ProcessRole, "description": input.Description, "parent_zone_id": input.ParentZoneID, "capacity": input.Capacity, "is_storable": input.IsStorable, "pick_sequence": input.PickSequence, "max_weight_kg": input.MaxWeightKg, "max_volume_m3": input.MaxVolumeM3, "inventory_frequency_days": input.InventoryFrequencyDays}
	for key, value := range replacements {
		v := reflect.ValueOf(value)
		if v.IsNil() {
			continue
		}
		replacement := v.Elem().Interface()
		if text, ok := replacement.(string); ok {
			replacement = strings.TrimSpace(text)
		}
		if number, ok := replacement.(int); ok {
			replacement = int64(number)
		}
		p.Draft[key] = replacement
	}
	for _, key := range input.ClearFields {
		if !oneOf(key, "description", "parent_zone_id", "capacity", "pick_sequence", "max_weight_kg", "max_volume_m3", "inventory_frequency_days") {
			p.require("clear_fields", "Dieses Feld kann nicht geleert werden.", key)
			continue
		}
		if !reflect.ValueOf(replacements[key]).IsNil() {
			p.require("clear_fields", "Ein Feld kann nicht gleichzeitig gesetzt und geleert werden.", key)
		}
		p.Draft[key] = nil
	}
	p.Draft["code"] = strings.ToUpper(nullableText(p.Draft["code"]))
	if nullableText(p.Draft["barcode"]) == "" {
		p.Draft["barcode"] = "LOC-" + nullableText(p.Draft["code"])
	}
	if nullableText(p.Draft["description"]) == "" {
		p.Draft["description"] = nil
	}
	p.Diff = map[string]map[string]any{}
	for _, key := range warehouseLocationEditableFields {
		if !reflect.DeepEqual(before[key], p.Draft[key]) {
			p.Diff[key] = map[string]any{"before": before[key], "after": p.Draft[key]}
		}
	}
	p.Draft["expected_updated_at"] = version
	if len(p.Diff) == 0 {
		p.require("changed_fields", "Welche Lagerplatzfelder sollen geändert werden?", nil)
	}
	if input.ConfirmUpdate && input.ExpectedUpdatedAt == "" || input.ExpectedUpdatedAt != "" && input.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Die genaue Version aus einer neuen Vorschau ist erforderlich.", version)
	}
	if p.Current["is_active"] != true || p.Current["operational_status"] == "archived" {
		p.require("is_active", "Archivierte Lagerplätze können nicht bearbeitet werden.", nil)
	}
	validateWarehouseLocationUpdateDraft(&p)
	if len(p.Missing) > 0 {
		p.finish()
		return p, nil
	}
	parent := p.Draft["parent_zone_id"]
	if parent != nil {
		ancestors, err := db.Query(ctx, `WITH RECURSIVE ancestors AS (SELECT zone_id AS id,parent_zone_id,code,name,is_active,operational_status FROM storage_zones WHERE zone_id=$1 UNION SELECT z.zone_id,z.parent_zone_id,z.code,z.name,z.is_active,z.operational_status FROM storage_zones z JOIN ancestors a ON z.zone_id=a.parent_zone_id) SELECT * FROM ancestors`, parent)
		if err != nil {
			return p, err
		}
		if len(ancestors) == 0 {
			p.require("parent_zone_id", "Elternknoten wurde nicht gefunden.", parent)
		}
		for _, row := range ancestors {
			p.RelatedRecords = append(p.RelatedRecords, row)
			if numericID(row["id"]) == input.ZoneID || row["is_active"] != true || row["operational_status"] == "archived" {
				p.require("parent_zone_id", "Elternknoten erzeugt einen Kreis oder liegt unter einem archivierten Bereich.", row)
			}
		}
	}
	usage, err := db.Query(ctx, `SELECT (SELECT COUNT(*) FROM devices WHERE zone_id=$1 AND status='in_storage' AND lifecycle_status='active') AS devices,(SELECT COUNT(*) FROM cases WHERE zone_id=$1) AS cases,COALESCE((SELECT SUM(quantity) FROM product_locations WHERE zone_id=$1),0) AS product_quantity`, input.ZoneID)
	if err != nil {
		return p, err
	}
	if len(usage) != 1 {
		return p, fmt.Errorf("location occupancy unavailable")
	}
	u := usage[0]
	used := numericFloat(u["devices"]) + numericFloat(u["cases"]) + numericFloat(u["product_quantity"])
	p.RelatedRecords = append(p.RelatedRecords, map[string]any{"id": input.ZoneID, "occupancy": used, "inventory": u})
	if _, changed := p.Diff["is_storable"]; changed && p.Draft["is_storable"] == false && used > 0 {
		p.require("is_storable", "Ein belegter Lagerplatz kann kein Strukturbereich werden.", u)
	}
	if _, changed := p.Diff["capacity"]; changed && p.Draft["capacity"] != nil && numericFloat(p.Draft["capacity"]) < used {
		p.require("capacity", "Neue Kapazität liegt unter der aktuellen Belegung.", u)
	}
	duplicates, err := db.Query(ctx, `SELECT zone_id AS id,code,name,barcode FROM storage_zones WHERE zone_id<>$1 AND (lower(trim(code))=lower($2) OR lower(trim(barcode))=lower($3) OR (lower(trim(name))=lower($4) AND parent_zone_id IS NOT DISTINCT FROM $5)) LIMIT 10`, input.ZoneID, p.Draft["code"], p.Draft["barcode"], p.Draft["name"], parent)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_location", "Code, Scan-Code oder Name unter demselben Elternknoten ist bereits vorhanden.", duplicates)
	}
	if _, changed := p.Diff["name"]; changed {
		candidates, err := warehouseMasterCandidates(ctx, db, "zone", nullableText(p.Draft["name"]), 20)
		if err != nil {
			return p, err
		}
		similar := []map[string]any{}
		for _, row := range candidates {
			if numericID(row["id"]) != input.ZoneID {
				similar = append(similar, row)
			}
		}
		p.RelatedRecords = append(p.RelatedRecords, similar...)
		if len(similar) > 0 && !input.AllowSimilar {
			p.require("similar_location_review", "Ähnliche Lagerplätze prüfen und allow_similar nur nach Freigabe setzen.", similar)
		}
	}
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	p.finish()
	return p, nil
}

func validateWarehouseLocationUpdateDraft(p *preparedMutation) {
	d := p.Draft
	for key, limit := range map[string]int{"code": 50, "name": 100, "barcode": 255} {
		text := nullableText(d[key])
		length := len(text)
		if key == "name" {
			length = len([]rune(text))
		}
		if text == "" || length > limit {
			p.require(key, "Pflichtfeld ist leer oder zu lang.", limit)
		}
	}
	if !oneOf(nullableText(d["type"]), "shelf", "rack", "case", "vehicle", "stage", "warehouse", "other") {
		p.require("type", "Ungültiger Lagerplatztyp.", nil)
	}
	if !oneOf(nullableText(d["location_kind"]), "site", "rack", "bin", "level", "vehicle", "area") {
		p.require("location_kind", "Ungültige Lagerplatzart.", nil)
	}
	if !oneOf(nullableText(d["process_role"]), "storage", "receiving", "return", "inspection", "quarantine", "repair", "charging", "picking", "staging", "shipping", "transport", "unknown") {
		p.require("process_role", "Ungültige Prozessrolle.", nil)
	}
	if d["capacity_mode"] != "item_count" {
		p.require("capacity_mode", "Nur item_count wird unterstützt.", nil)
	}
	for _, key := range []string{"capacity", "max_weight_kg", "max_volume_m3"} {
		if d[key] != nil && (numericFloat(d[key]) <= 0 || math.IsNaN(numericFloat(d[key])) || math.IsInf(numericFloat(d[key]), 0)) {
			p.require(key, "Grenzwert muss positiv und endlich sein.", nil)
		}
	}
	if d["capacity"] != nil {
		v := numericFloat(d["capacity"])
		if math.Trunc(v) != v || v > math.MaxInt32 {
			p.require("capacity", "Kapazität benötigt eine positive 32-Bit-Ganzzahl.", nil)
		}
	}
	for key, maximum := range map[string]float64{"max_weight_kg": 999999999.999, "max_volume_m3": 999999.999999} {
		if d[key] != nil && numericFloat(d[key]) > maximum {
			p.require(key, "Grenzwert überschreitet den speicherbaren Wert.", maximum)
		}
	}
	if d["parent_zone_id"] != nil && (numericID(d["parent_zone_id"]) <= 0 || numericID(d["parent_zone_id"]) > math.MaxInt32) {
		p.require("parent_zone_id", "Eltern-ID muss positiv sein.", nil)
	}
	for _, key := range []string{"pick_sequence", "inventory_frequency_days"} {
		if d[key] != nil {
			v := numericID(d[key])
			if v > math.MaxInt32 || v < math.MinInt32 || key == "inventory_frequency_days" && v < 0 {
				p.require(key, "Ungültiger 32-Bit-Ganzzahlwert.", nil)
			}
		}
	}
}

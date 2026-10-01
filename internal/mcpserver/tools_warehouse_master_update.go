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

type WarehouseManufacturerUpdateInput struct {
	MutationControl
	ManufacturerID    int64   `json:"manufacturer_id,omitempty" jsonschema:"Exact existing manufacturer ID."`
	Name              *string `json:"name,omitempty" jsonschema:"Replacement name, 1-255 characters."`
	Website           *string `json:"website,omitempty" jsonschema:"Replacement HTTP(S) website, at most 255 characters; empty clears it. A hostname gains https://."`
	AllowSimilar      bool    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar names."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool    `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the full diff and receiving explicit confirmation."`
}

type WarehouseBrandUpdateInput struct {
	MutationControl
	BrandID           int64   `json:"brand_id,omitempty" jsonschema:"Exact existing brand ID."`
	Name              *string `json:"name,omitempty" jsonschema:"Replacement name, 1-255 characters, unique within its manufacturer."`
	ManufacturerID    *int64  `json:"manufacturer_id,omitempty" jsonschema:"Existing manufacturer ID. Linked products must already use this manufacturer."`
	ClearManufacturer bool    `json:"clear_manufacturer,omitempty" jsonschema:"Explicitly remove the association. Do not also supply manufacturer_id; linked products must already have no manufacturer."`
	AllowSimilar      bool    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar names."`
	ExpectedUpdatedAt string  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; required for confirmed execution."`
	ConfirmUpdate     bool    `json:"confirm_update,omitempty" jsonschema:"Set only after presenting the full diff and receiving explicit confirmation."`
}

func registerWarehouseMasterUpdateTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	registerWarehouseMasterUpdatePair(server, cfg, api, "manufacturer", func(ctx context.Context, input WarehouseManufacturerUpdateInput) (preparedMutation, error) {
		return prepareWarehouseMasterUpdate(ctx, db, "manufacturer", input.ManufacturerID, input.Name, input.Website, nil, false, input.AllowSimilar, input.ExpectedUpdatedAt, input.ConfirmUpdate)
	}, func(input WarehouseManufacturerUpdateInput) (int64, bool) {
		return input.ManufacturerID, input.ConfirmUpdate
	})
	registerWarehouseMasterUpdatePair(server, cfg, api, "brand", func(ctx context.Context, input WarehouseBrandUpdateInput) (preparedMutation, error) {
		return prepareWarehouseMasterUpdate(ctx, db, "brand", input.BrandID, input.Name, nil, input.ManufacturerID, input.ClearManufacturer, input.AllowSimilar, input.ExpectedUpdatedAt, input.ConfirmUpdate)
	}, func(input WarehouseBrandUpdateInput) (int64, bool) { return input.BrandID, input.ConfirmUpdate })
}

func registerWarehouseMasterUpdatePair[In any](server *mcp.Server, cfg config.Config, api *coreAPIClient, entity string, prepare func(context.Context, In) (preparedMutation, error), identity func(In) (int64, bool)) {
	plural := entity + "s"
	sources := func(id int64) []Source {
		return []Source{{Service: "warehousecore", Entity: entity, ID: fmt.Sprint(id)}}
	}
	addWritePreparationTool(server, "warehouse."+plural+".prepare_update", "Prepare warehouse "+entity+" update", "Preview all editable fields, exact version, full diff, duplicate names and affected record counts for a Warehouse administrator. Brand reassociation must preserve linked product consistency.", func(ctx context.Context, input In) (any, []Source, []string, error) {
		p, err := prepare(ctx, input)
		id, _ := identity(input)
		return p.response("draft"), sources(id), p.Warnings, err
	})
	addUpdateTool(server, "warehouse."+plural+".update", "Update warehouse "+entity, "Update one manufacturer or brand after full preview, exact version and explicit confirmation. WarehouseCore rechecks dependencies and commits audit and durable replay receipt atomically.", func(ctx context.Context, input In) (any, []Source, []string, error) {
		p, err := prepare(ctx, input)
		id, confirmed := identity(input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), sources(id), p.Warnings, err
		}
		if !confirmed {
			return p.response("confirmation_required"), sources(id), append(p.Warnings, "No data was changed. Present the complete diff and obtain explicit confirmation."), nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/%s/%d", plural, id), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", entity: updated, "diff": p.Diff}, sources(id), p.Warnings, nil
	})
}

func prepareWarehouseMasterUpdate(ctx context.Context, db *store.Store, entity string, id int64, name, website *string, manufacturerID *int64, clearManufacturer, allowSimilar bool, expected string, confirmed bool) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	if id <= 0 || id > math.MaxInt32 {
		p.require(entity+"_id", "Welche gültige Stammdaten-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	query := `SELECT manufacturerid AS id,name,website,lifecycle_status,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM manufacturer WHERE manufacturerid=$1`
	if entity == "brand" {
		query = `SELECT brandid AS id,name,lifecycle_status,manufacturerid AS manufacturer_id,to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM brands WHERE brandid=$1`
	}
	rows, err := db.Query(ctx, query, id)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require(entity+"_id", "Stammdatensatz wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	if p.Current["lifecycle_status"] == "archived" {
		p.require("restore_required", "Archivierten Stammdatensatz zuerst wiederherstellen.", nil)
	}
	version := nullableText(p.Current["updated_at"])
	p.Draft["name"] = p.Current["name"]
	if entity == "manufacturer" {
		p.Draft["website"] = p.Current["website"]
	} else {
		p.Draft["manufacturer_id"] = p.Current["manufacturer_id"]
	}
	before := cloneMap(p.Draft)
	p.Draft["name"] = strings.TrimSpace(nullableText(p.Draft["name"]))
	if name != nil {
		p.Draft["name"] = strings.TrimSpace(*name)
	}
	if entity == "manufacturer" {
		value := nullableText(p.Draft["website"])
		if website != nil {
			value = *website
		}
		normalized, websiteErr := warehouseManufacturerWebsite(value)
		if websiteErr != nil {
			p.require("website", "HTTP(S)-Website mit maximal 255 Zeichen erforderlich.", nil)
		}
		p.Draft["website"] = nil
		if normalized != nil {
			p.Draft["website"] = *normalized
		}
	} else {
		if manufacturerID != nil {
			p.Draft["manufacturer_id"] = *manufacturerID
		}
		if clearManufacturer {
			if manufacturerID != nil {
				p.require("clear_manufacturer", "Hersteller nicht gleichzeitig setzen und entfernen.", nil)
			}
			p.Draft["manufacturer_id"] = nil
		}
	}
	text := nullableText(p.Draft["name"])
	if text == "" || len([]rune(text)) > 255 {
		p.require("name", "Name benötigt 1-255 Zeichen.", nil)
	}
	p.Diff = map[string]map[string]any{}
	for key, value := range before {
		if !reflect.DeepEqual(value, p.Draft[key]) {
			p.Diff[key] = map[string]any{"before": value, "after": p.Draft[key]}
		}
	}
	p.Draft["expected_updated_at"] = version
	if len(p.Diff) == 0 {
		p.require("changed_fields", "Welche Stammdatenfelder sollen geändert werden?", nil)
	}
	if confirmed && expected == "" || expected != "" && expected != version {
		p.require("expected_updated_at", "Die genaue Version aus einer neuen Vorschau ist erforderlich.", version)
	}
	var duplicateQuery, usageQuery string
	args := []any{id, text}
	if entity == "manufacturer" {
		duplicateQuery = `SELECT manufacturerid AS id,name FROM manufacturer WHERE manufacturerid<>$1 AND lower(trim(name))=lower($2) LIMIT 10`
		usageQuery = `SELECT (SELECT COUNT(*) FROM products WHERE manufacturerid=$1) AS product_count,(SELECT COUNT(*) FROM brands WHERE manufacturerid=$1) AS brand_count`
	} else {
		parent := p.Draft["manufacturer_id"]
		if parent != nil {
			if numericID(parent) <= 0 || numericID(parent) > math.MaxInt32 {
				p.require("manufacturer_id", "Hersteller-ID muss eine positive 32-Bit-Ganzzahl sein.", nil)
				p.finish()
				return p, nil
			} else {
				parents, err := db.Query(ctx, `SELECT manufacturerid AS id,name FROM manufacturer WHERE manufacturerid=$1 AND lifecycle_status='active'`, parent)
				if err != nil {
					return p, err
				}
				p.RelatedRecords = append(p.RelatedRecords, parents...)
				if len(parents) != 1 {
					p.require("manufacturer_id", "Hersteller wurde nicht gefunden.", parent)
				}
			}
		}
		if _, changed := p.Diff["manufacturer_id"]; changed {
			conflicts, err := db.Query(ctx, `SELECT productid AS id,name,manufacturerid AS manufacturer_id FROM products WHERE brandid=$1 AND manufacturerid IS DISTINCT FROM $2::int ORDER BY productid LIMIT 10`, id, parent)
			if err != nil {
				return p, err
			}
			if len(conflicts) > 0 {
				p.RelatedRecords = append(p.RelatedRecords, conflicts...)
				p.require("linked_products", "Verknüpfte Produkte benötigen ihren bisherigen Hersteller. Zuerst Produktzuordnungen bearbeiten.", conflicts)
			}
		}
		duplicateQuery = `SELECT brandid AS id,name FROM brands WHERE brandid<>$1 AND lower(trim(name))=lower($2) AND manufacturerid IS NOT DISTINCT FROM $3::int LIMIT 10`
		args = append(args, parent)
		usageQuery = `SELECT COUNT(*) AS product_count FROM products WHERE brandid=$1`
	}
	duplicates, err := db.Query(ctx, duplicateQuery, args...)
	if err != nil {
		return p, err
	}
	if len(duplicates) > 0 {
		p.RelatedRecords = append(p.RelatedRecords, duplicates...)
		p.require("duplicate_"+entity, "Name ist bereits vorhanden.", duplicates)
	}
	usage, err := db.Query(ctx, usageQuery, id)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = append(p.RelatedRecords, usage...)
	if _, changed := p.Diff["name"]; changed {
		candidates, err := warehouseMasterCandidates(ctx, db, entity, text, 20)
		if err != nil {
			return p, err
		}
		similar := []map[string]any{}
		for _, row := range candidates {
			if numericID(row["id"]) != id {
				similar = append(similar, row)
			}
		}
		p.RelatedRecords = append(p.RelatedRecords, similar...)
		if len(similar) > 0 && !allowSimilar {
			p.require("similar_"+entity+"_review", "Ähnliche Namen prüfen und allow_similar nur nach Freigabe setzen.", similar)
		}
	}
	p.Warnings = append(p.Warnings, "Renaming shared master data changes its displayed name on linked records.")
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	p.finish()
	return p, nil
}

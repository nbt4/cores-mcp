package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehousePackageItem struct {
	ProductID  int64 `json:"product_id" jsonschema:"Exact existing active Warehouse product ID; each product may occur once."`
	Quantity   int64 `json:"quantity" jsonschema:"Integer quantity from 1 to 1000000."`
	IsOptional bool  `json:"is_optional,omitempty" jsonschema:"Whether this product line is an optional package component."`
}
type WarehousePackageCreateInput struct {
	MutationControl
	Name            string                 `json:"name,omitempty" jsonschema:"Unique package name, 1-255 characters."`
	Description     string                 `json:"description,omitempty" jsonschema:"Optional description, at most 4000 characters."`
	Price           *float64               `json:"price,omitempty" jsonschema:"Optional price in EUR, nonnegative with at most two decimals, maximum 99999999.99."`
	Category        string                 `json:"category,omitempty" jsonschema:"Optional package category text, at most 100 characters."`
	WebsiteVisible  bool                   `json:"website_visible,omitempty" jsonschema:"Whether the package is publicly visible on the website."`
	Aliases         []string               `json:"aliases,omitempty" jsonschema:"At most 50 aliases of at most 160 characters; blank and case-insensitive duplicate aliases are removed."`
	Items           []WarehousePackageItem `json:"items,omitempty" jsonschema:"Complete package contents, 1-200 distinct active products. No stock or device movement is performed."`
	AllowSimilar    bool                   `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar package names."`
	ConfirmCreation bool                   `json:"confirm_creation,omitempty" jsonschema:"Set only after showing the complete draft and obtaining explicit confirmation."`
}
type WarehousePackageUpdateInput struct {
	MutationControl
	PackageID         int64                   `json:"package_id,omitempty" jsonschema:"Exact existing active package ID. ID and package code remain unchanged."`
	Name              *string                 `json:"name,omitempty" jsonschema:"Replacement unique name, 1-255 characters."`
	Description       *string                 `json:"description,omitempty" jsonschema:"Replacement description, at most 4000 characters; empty clears it."`
	Price             *float64                `json:"price,omitempty" jsonschema:"Replacement EUR price, nonnegative with at most two decimals, maximum 99999999.99. Job use blocks price changes."`
	Category          *string                 `json:"category,omitempty" jsonschema:"Replacement category text, at most 100 characters; empty clears it."`
	WebsiteVisible    *bool                   `json:"website_visible,omitempty" jsonschema:"Replacement public website visibility."`
	Aliases           *[]string               `json:"aliases,omitempty" jsonschema:"Complete replacement aliases, at most 50 of at most 160 characters; [] clears aliases."`
	Items             *[]WarehousePackageItem `json:"items,omitempty" jsonschema:"Complete replacement contents, 1-200 distinct active products; omitted preserves every line. Job use blocks content changes."`
	ClearFields       []string                `json:"clear_fields,omitempty" jsonschema:"Explicitly clear nullable description, price or category. Do not also provide the same field."`
	AllowSimilar      bool                    `json:"allow_similar,omitempty" jsonschema:"Set only after reviewing similar package names."`
	ExpectedUpdatedAt string                  `json:"expected_updated_at,omitempty" jsonschema:"Exact version from prepare_update; includes metadata and item edits."`
	ConfirmUpdate     bool                    `json:"confirm_update,omitempty" jsonschema:"Set only after showing the complete diff and receiving explicit confirmation."`
}
type warehousePackageDraft struct {
	Name           string                 `json:"name"`
	Description    *string                `json:"description"`
	Price          *float64               `json:"price"`
	Category       *string                `json:"category"`
	WebsiteVisible bool                   `json:"website_visible"`
	Aliases        []string               `json:"aliases"`
	Items          []WarehousePackageItem `json:"items"`
}

func registerWarehousePackageTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	source := func(id int64) []Source {
		if id <= 0 {
			return []Source{{Service: "warehousecore", Entity: "package_draft"}}
		}
		return []Source{{Service: "warehousecore", Entity: "package", ID: fmt.Sprint(id)}}
	}
	addWritePreparationTool(server, "warehouse.packages.prepare_create", "Prepare product package", "Preview all package metadata and complete contents, validate active product IDs and quantities, and review duplicate/similar names. Requires Warehouse administrator.", func(ctx context.Context, in WarehousePackageCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehousePackageCreate(ctx, db, in)
		return p.response("draft"), source(0), p.Warnings, err
	})
	addCreateTool(server, "warehouse.packages.create", "Create product package", "Create one package and its product lines atomically with audit and durable replay receipt after complete preview and explicit confirmation. No stock movement or mirror product is created.", func(ctx context.Context, in WarehousePackageCreateInput) (any, []Source, []string, error) {
		p, err := prepareWarehousePackageCreate(ctx, db, in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), source(0), p.Warnings, err
		}
		if !in.ConfirmCreation {
			return p.response("confirmation_required"), source(0), p.Warnings, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/product-packages", http.MethodPost, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "created", "package": result}, source(numericID(result["package_id"])), p.Warnings, nil
	})
	addWritePreparationTool(server, "warehouse.packages.prepare_update", "Prepare product package update", "Load all editable fields and complete contents, exact version, full diff and job use. Package prices and contents used in any job cannot be changed; metadata remains editable.", func(ctx context.Context, in WarehousePackageUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehousePackageUpdate(ctx, db, in)
		return p.response("draft"), source(in.PackageID), p.Warnings, err
	})
	addUpdateTool(server, "warehouse.packages.update", "Update product package", "Update an active package after full diff, exact metadata/content version and explicit confirmation. Owning Core rechecks references and atomically commits edit, audit and replay receipt. IDs/code and existing job composition are preserved.", func(ctx context.Context, in WarehousePackageUpdateInput) (any, []Source, []string, error) {
		p, err := prepareWarehousePackageUpdate(ctx, db, in)
		if err != nil || !p.Ready {
			return p.response("needs_input"), source(in.PackageID), p.Warnings, err
		}
		if !in.ConfirmUpdate {
			return p.response("confirmation_required"), source(in.PackageID), p.Warnings, nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, fmt.Sprintf("/api/v1/admin/product-packages/%d", in.PackageID), http.MethodPut, p.Draft, &result); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "package": result, "diff": p.Diff}, source(in.PackageID), p.Warnings, nil
	})
}

func packageDraftMap(d warehousePackageDraft) map[string]any {
	return map[string]any{"name": d.Name, "description": d.Description, "price": d.Price, "category": d.Category, "website_visible": d.WebsiteVisible, "aliases": d.Aliases, "items": d.Items}
}
func prepareWarehousePackageCreate(ctx context.Context, db *store.Store, in WarehousePackageCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	d := warehousePackageDraft{Name: in.Name, Description: &in.Description, Price: in.Price, Category: &in.Category, WebsiteVisible: in.WebsiteVisible, Aliases: in.Aliases, Items: append([]WarehousePackageItem(nil), in.Items...)}
	err := validateWarehousePackageDraft(ctx, db, &p, &d, 0, in.AllowSimilar)
	p.Draft = packageDraftMap(d)
	p.finish()
	return p, err
}
func prepareWarehousePackageUpdate(ctx context.Context, db *store.Store, in WarehousePackageUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if err := requireWarehouseMasterAdmin(ctx); err != nil {
		return p, err
	}
	if in.PackageID <= 0 || in.PackageID > math.MaxInt32 {
		p.require("package_id", "Welche gültige Paket-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT id,COALESCE(NULLIF(package_code,''),code,'') AS package_code,COALESCE(is_active,true) AS is_active,
 to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,
 jsonb_build_object('name',name,'description',description,'price',price,'category',category,'website_visible',COALESCE(website_visible,false),'aliases',COALESCE(NULLIF(alias_json,''),'[]')::jsonb,
 'items',COALESCE((SELECT jsonb_agg(jsonb_build_object('product_id',product_id,'quantity',COALESCE(quantity,1),'is_optional',COALESCE(is_optional,false)) ORDER BY product_id,id) FROM product_package_items WHERE package_id=$1),'[]'::jsonb)) AS fields,
 (SELECT count(*) FROM job_packages WHERE package_id=$1) AS job_count
 FROM product_packages WHERE id=$1`, in.PackageID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("package_id", "Paket wurde nicht gefunden.", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	if p.Current["is_active"] != true {
		p.require("package_id", "Nur aktive Pakete sind über diesen Update-Pfad bearbeitbar.", nil)
	}
	var current warehousePackageDraft
	raw, err := json.Marshal(p.Current["fields"])
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(raw, &current); err != nil {
		return p, err
	}
	d := current
	d.Items = append([]WarehousePackageItem(nil), current.Items...)
	d.Aliases = append([]string(nil), current.Aliases...)
	if in.Name != nil {
		d.Name = *in.Name
	}
	if in.Description != nil {
		d.Description = in.Description
	}
	if in.Price != nil {
		d.Price = in.Price
	}
	if in.Category != nil {
		d.Category = in.Category
	}
	if in.WebsiteVisible != nil {
		d.WebsiteVisible = *in.WebsiteVisible
	}
	if in.Aliases != nil {
		d.Aliases = append([]string(nil), (*in.Aliases)...)
	}
	if in.Items != nil {
		d.Items = append([]WarehousePackageItem(nil), (*in.Items)...)
	}
	for _, field := range in.ClearFields {
		switch field {
		case "description":
			if in.Description != nil {
				p.require("description", "Leeren oder einen Wert setzen, nicht beides.", nil)
			}
			d.Description = nil
		case "price":
			if in.Price != nil {
				p.require("price", "Leeren oder einen Wert setzen, nicht beides.", nil)
			}
			d.Price = nil
		case "category":
			if in.Category != nil {
				p.require("category", "Leeren oder einen Wert setzen, nicht beides.", nil)
			}
			d.Category = nil
		default:
			p.require("clear_fields", "Nur description, price und category können geleert werden.", nil)
		}
	}
	version := nullableText(p.Current["updated_at"])
	if in.ConfirmUpdate && in.ExpectedUpdatedAt == "" || in.ExpectedUpdatedAt != "" && in.ExpectedUpdatedAt != version {
		p.require("expected_updated_at", "Neue Vorschau mit unveränderter exakter Version erforderlich.", version)
	}
	if err = validateWarehousePackageDraft(ctx, db, &p, &d, in.PackageID, in.AllowSimilar); err != nil {
		return p, err
	}
	before, after := packageDraftMap(current), packageDraftMap(d)
	p.Diff = map[string]map[string]any{}
	for field, value := range after {
		if !reflect.DeepEqual(before[field], value) {
			p.Diff[field] = map[string]any{"before": before[field], "after": value}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Paketfelder sollen geändert werden?", nil)
	}
	if numericID(p.Current["job_count"]) > 0 && (p.Diff["items"] != nil || p.Diff["price"] != nil) {
		p.require("job_dependencies", "Preis und Inhalt bereits in Jobs verwendeter Pakete müssen erhalten bleiben. Neues Paket anlegen.", p.Current["job_count"])
	}
	p.Draft = after
	p.Draft["expected_updated_at"] = version
	p.finish()
	return p, nil
}

func validateWarehousePackageDraft(ctx context.Context, db *store.Store, p *preparedMutation, d *warehousePackageDraft, excludeID int64, allowSimilar bool) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > 255 {
		p.require("name", "Eindeutiger Paketname mit 1–255 Zeichen erforderlich.", nil)
	}
	for _, f := range []struct {
		ptr   **string
		max   int
		field string
	}{{&d.Description, 4000, "description"}, {&d.Category, 100, "category"}} {
		if *f.ptr != nil {
			v := strings.TrimSpace(**f.ptr)
			if len([]rune(v)) > f.max {
				p.require(f.field, "Text ist zu lang.", nil)
			}
			if v == "" {
				*f.ptr = nil
			} else {
				*f.ptr = &v
			}
		}
	}
	if d.Price != nil {
		v := *d.Price
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 99999999.99 || math.Abs(v*100-math.Round(v*100)) > 0.00001 {
			p.require("price", "Nichtnegativer EUR-Preis mit höchstens zwei Nachkommastellen, maximal 99999999.99 erforderlich.", nil)
		}
	}
	if len(d.Aliases) > 50 {
		p.require("aliases", "Höchstens 50 Aliasse erlaubt.", nil)
	}
	aliases := make([]string, 0, len(d.Aliases))
	seenAliases := map[string]bool{}
	for _, alias := range d.Aliases {
		v := strings.TrimSpace(alias)
		key := strings.ToLower(v)
		if len([]rune(v)) > 160 {
			p.require("aliases", "Aliasse dürfen höchstens 160 Zeichen enthalten.", nil)
		}
		if v != "" && !seenAliases[key] {
			aliases = append(aliases, v)
			seenAliases[key] = true
		}
	}
	d.Aliases = aliases
	if len(d.Items) == 0 || len(d.Items) > 200 {
		p.require("items", "Vollständige Liste von 1–200 verschiedenen aktiven Produkten erforderlich.", nil)
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, item := range d.Items {
		if item.ProductID <= 0 || item.ProductID > math.MaxInt32 || item.Quantity <= 0 || item.Quantity > 1000000 || seen[item.ProductID] {
			p.require("items", "Eindeutige gültige Produkt-IDs und Mengen von 1 bis 1000000 erforderlich.", nil)
		} else {
			ids = append(ids, item.ProductID)
		}
		seen[item.ProductID] = true
	}
	sort.Slice(d.Items, func(i, j int) bool { return d.Items[i].ProductID < d.Items[j].ProductID })
	if len(ids) > 0 && len(ids) <= 200 {
		raw, _ := json.Marshal(ids)
		products, err := db.Query(ctx, `SELECT productid AS id,name,COALESCE(lifecycle_status,'active') AS lifecycle_status FROM products WHERE productid IN (SELECT value::int FROM jsonb_array_elements_text($1::jsonb)) ORDER BY productid`, string(raw))
		if err != nil {
			return err
		}
		if len(products) != len(ids) {
			p.require("items", "Alle Produkt-IDs müssen vorhanden sein; Trefferliste darf nicht abgeschnitten sein.", nil)
		}
		for _, product := range products {
			if product["lifecycle_status"] != "active" {
				p.require("items", "Paketbestandteile müssen aktive Produkte sein.", product)
			}
		}
		p.RelatedRecords = append(p.RelatedRecords, products...)
	}
	if d.Name != "" && len([]rune(d.Name)) <= 255 {
		candidates, err := db.Query(ctx, `SELECT id,name,COALESCE(is_active,true) AS is_active FROM product_packages WHERE id<>$1 AND (lower(trim(name))=lower($2) OR name ILIKE $3) ORDER BY name LIMIT 100`, excludeID, d.Name, "%"+strings.Fields(d.Name)[0]+"%")
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			name := nullableText(candidate["name"])
			if strings.EqualFold(strings.TrimSpace(name), d.Name) {
				p.require("duplicate_package", "Ein Paket mit diesem Namen existiert bereits, auch archivierte Namen sind geschützt.", candidate)
			} else if warehouseMatchScore(d.Name, name, "") >= 50 && !allowSimilar {
				p.require("similar_package_review", "Ähnliche Pakete prüfen und Wiederverwendung oder ausdrückliche Neuanlage entscheiden.", candidate)
			}
		}
		if len(candidates) >= db.Limit(100) {
			p.require("package_search_limit", "Paketname genauer eingrenzen; Kandidatenliste erreicht das Abfragelimit.", nil)
		}
		p.RelatedRecords = append(p.RelatedRecords, candidates...)
	}
	p.Warnings = append(p.Warnings, untrustedTextWarning()...)
	if d.WebsiteVisible {
		p.Warnings = append(p.Warnings, "Package name, description, category, contents and price become publicly visible on the website.")
	}
	p.Warnings = append(p.Warnings, "Package changes do not move stock or devices. Existing job prices and contents are protected.")
	return nil
}

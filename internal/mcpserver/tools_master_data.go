package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type MasterDataResolveInput struct {
	Entity string `json:"entity" jsonschema:"One of warehouse.manufacturer, warehouse.brand, warehouse.category, warehouse.subcategory, warehouse.third_category, warehouse.count_type, warehouse.zone, procurement.supplier, procurement.category, rental.customer, or rental.venue."`
	Query  string `json:"query" jsonschema:"Name, code, or spelling variant to resolve before preparing creation."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum ranked candidates; defaults to 10 and is capped at 20."`
}

type masterDataSource struct {
	service string
	entity  string
	query   string
}

var masterDataSources = map[string]masterDataSource{
	"procurement.supplier": {"procurementcore", "supplier", `SELECT id,name,code AS context,code FROM proc_suppliers WHERE active=true ORDER BY name LIMIT 500`},
	"procurement.category": {"procurementcore", "category", `SELECT id,name,'' AS context FROM proc_categories ORDER BY name LIMIT 500`},
	"rental.customer":      {"rentalcore", "customer", `SELECT customerid AS id,COALESCE(NULLIF(companyname,''),NULLIF(name,''),TRIM(CONCAT_WS(' ',firstname,lastname))) AS name,concat_ws(' · ',city,country) AS context FROM customers WHERE COALESCE(is_archived,false)=false ORDER BY companyname,name LIMIT 500`},
	"rental.venue":         {"rentalcore", "venue", `SELECT id,name,concat_ws(' · ',city,zip) AS context FROM venues ORDER BY name LIMIT 500`},
}

var warehouseMasterEntities = map[string]string{
	"warehouse.manufacturer":   "manufacturer",
	"warehouse.brand":          "brand",
	"warehouse.category":       "category",
	"warehouse.subcategory":    "subcategory",
	"warehouse.third_category": "third_category",
	"warehouse.count_type":     "count_type",
	"warehouse.zone":           "zone",
}

func registerMasterDataTools(server *mcp.Server, db *store.Store) {
	addTool(server, "cores.master_data.resolve", "Resolve Cores master data", "Fuzzy-match existing master data across Cores before preparing creation. Exact matches are selected; similar or ambiguous candidates require user review. No data is created.", func(ctx context.Context, input MasterDataResolveInput) (any, []Source, []string, error) {
		entity := strings.TrimSpace(input.Entity)
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return nil, nil, nil, errorsNew("query is required")
		}
		var (
			candidates []map[string]any
			service    string
			sourceName string
			err        error
			warnings   []string
			incomplete bool
		)
		if warehouseEntity, ok := warehouseMasterEntities[entity]; ok {
			service = "warehousecore"
			sourceName = warehouseEntity
			candidates, err = warehouseMasterCandidates(ctx, db, warehouseEntity, query, input.Limit)
		} else if source, ok := masterDataSources[entity]; ok {
			service = source.service
			sourceName = source.entity
			var rows []map[string]any
			rows, err = db.Query(ctx, source.query)
			if err == nil {
				candidates = rankMasterCandidates(rows, query, input.Limit)
				if len(rows) >= db.Limit(500) {
					incomplete = true
					warnings = append(warnings, "Candidate pool reached the server row limit; use the entity search tool before selecting or creating a record.")
				}
			}
		} else {
			return nil, nil, nil, fmt.Errorf("unsupported master-data entity %q", entity)
		}
		if err != nil {
			return nil, nil, nil, err
		}
		resolution, selected := classifyMasterCandidates(candidates)
		if incomplete {
			resolution, selected = "incomplete", nil
		}
		sources := make([]Source, 0, len(candidates))
		for _, candidate := range candidates {
			sources = append(sources, Source{Service: service, Entity: sourceName, ID: fmt.Sprint(candidate["id"])})
		}
		if len(sources) == 0 {
			sources = []Source{{Service: service, Entity: sourceName}}
		}
		return map[string]any{"entity": entity, "query": query, "resolution": resolution, "selected": selected, "candidates": candidates}, sources, warnings, nil
	})
}

func rankMasterCandidates(rows []map[string]any, query string, limit int) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		name := fmt.Sprint(row["name"])
		context := fmt.Sprint(row["context"])
		score := warehouseMatchScore(query, name, context)
		if code, ok := row["code"]; ok && normalizeIdentity(query) == normalizeIdentity(fmt.Sprint(code)) {
			score = 100
		}
		if score < 30 {
			continue
		}
		candidate := cloneMap(row)
		candidate["match_score"] = score
		candidate["match_kind"] = "similar"
		if normalizeIdentity(query) == normalizeIdentity(name) || score == 100 {
			candidate["match_kind"] = "exact"
		}
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i]["match_score"].(int), result[j]["match_score"].(int)
		if left == right {
			return fmt.Sprint(result[i]["name"]) < fmt.Sprint(result[j]["name"])
		}
		return left > right
	})
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func classifyMasterCandidates(candidates []map[string]any) (string, map[string]any) {
	var selected map[string]any
	exact := 0
	for _, candidate := range candidates {
		if candidate["match_kind"] == "exact" {
			exact++
			selected = candidate
		}
	}
	switch {
	case exact == 1:
		return "exact", selected
	case exact > 1, len(candidates) > 1:
		return "ambiguous", nil
	case len(candidates) == 1:
		return "similar", nil
	default:
		return "not_found", nil
	}
}

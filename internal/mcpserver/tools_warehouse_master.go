package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseMasterResolveInput struct {
	Entity string `json:"entity" jsonschema:"One of manufacturer, brand, category, subcategory, third_category, count_type, or zone."`
	Query  string `json:"query" jsonschema:"Name, abbreviation, code, or spelling variant to resolve."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum ranked candidates; defaults to 10 and is capped at 20."`
}

func registerWarehouseMasterTools(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.master_data.resolve", "Resolve warehouse master data", "Fuzzy-match manufacturers, brands, category levels, count types or storage zones. The result distinguishes one exact match, similar candidates, ambiguity and not found without creating data.", func(ctx context.Context, input WarehouseMasterResolveInput) (any, []Source, []string, error) {
		candidates, err := warehouseMasterCandidates(ctx, db, input.Entity, input.Query, input.Limit)
		if err != nil {
			return nil, nil, nil, err
		}
		resolution := "not_found"
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
			resolution = "exact"
		case exact > 1:
			resolution, selected = "ambiguous", nil
		case len(candidates) == 1:
			resolution = "similar"
		case len(candidates) > 1:
			resolution = "ambiguous"
		}
		sources := make([]Source, 0, len(candidates))
		for _, candidate := range candidates {
			sources = append(sources, Source{Service: "warehousecore", Entity: strings.TrimSpace(input.Entity), ID: fmt.Sprint(candidate["id"])})
		}
		if len(sources) == 0 {
			sources = []Source{{Service: "warehousecore", Entity: strings.TrimSpace(input.Entity)}}
		}
		return map[string]any{"entity": input.Entity, "query": strings.TrimSpace(input.Query), "resolution": resolution, "selected": selected, "candidates": candidates}, sources, nil, nil
	})
}

func warehouseMasterCandidates(ctx context.Context, db *store.Store, entity, query string, limit int) ([]map[string]any, error) {
	entity = strings.TrimSpace(entity)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errorsNew("query is required")
	}
	var statement string
	switch entity {
	case "manufacturer":
		statement = `SELECT manufacturerid AS id,name,COALESCE(website,'') AS context FROM manufacturer ORDER BY name LIMIT 500`
	case "brand":
		statement = `SELECT b.brandid AS id,b.name,concat_ws(' · ',m.name,b.manufacturerid::text) AS context,b.manufacturerid AS manufacturer_id,m.name AS manufacturer FROM brands b LEFT JOIN manufacturer m ON m.manufacturerid=b.manufacturerid ORDER BY b.name LIMIT 500`
	case "category":
		statement = `SELECT categoryid AS id,name,abbreviation AS context,abbreviation FROM categories ORDER BY name LIMIT 500`
	case "subcategory":
		statement = `SELECT s.subcategoryid AS id,s.name,concat_ws(' · ',c.name,s.abbreviation) AS context,s.abbreviation,s.categoryid AS category_id,c.name AS category FROM subcategories s JOIN categories c ON c.categoryid=s.categoryid ORDER BY s.name LIMIT 500`
	case "third_category":
		statement = `SELECT t.subbiercategoryid AS id,t.name,concat_ws(' · ',c.name,s.name,t.abbreviation) AS context,t.abbreviation,t.subcategoryid AS subcategory_id,s.name AS subcategory,s.categoryid AS category_id,c.name AS category FROM subbiercategories t JOIN subcategories s ON s.subcategoryid=t.subcategoryid JOIN categories c ON c.categoryid=s.categoryid ORDER BY t.name LIMIT 500`
	case "count_type":
		statement = `SELECT count_type_id AS id,name,abbreviation AS context,abbreviation FROM count_types ORDER BY name LIMIT 500`
	case "zone":
		statement = `SELECT zone_id AS id,name,concat_ws(' · ',code,location,process_role) AS context,code FROM storage_zones WHERE is_active=true ORDER BY name LIMIT 500`
	default:
		return nil, fmt.Errorf("unsupported warehouse master entity %q", entity)
	}
	rows, err := db.Query(ctx, statement)
	if err != nil {
		return nil, err
	}
	ranked := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		score := warehouseMatchScore(query, fmt.Sprint(row["name"]), fmt.Sprint(row["context"]))
		if score < 30 {
			continue
		}
		candidate := cloneMap(row)
		candidate["match_score"] = score
		candidate["match_kind"] = "similar"
		if normalizeIdentity(query) == normalizeIdentity(fmt.Sprint(row["name"])) {
			candidate["match_kind"] = "exact"
		}
		ranked = append(ranked, candidate)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := ranked[i]["match_score"].(int), ranked[j]["match_score"].(int)
		if left == right {
			return fmt.Sprint(ranked[i]["name"]) < fmt.Sprint(ranked[j]["name"])
		}
		return left > right
	})
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func warehouseMatchScore(query, name, context string) int {
	needle := normalizeIdentity(query)
	label := normalizeIdentity(name)
	if needle == "" || label == "" {
		return 0
	}
	if needle == label {
		return 100
	}
	if strings.Contains(label, needle) || strings.Contains(needle, label) {
		shorter, longer := len([]rune(needle)), len([]rune(label))
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		return 75 + 20*shorter/maxInt(longer, 1)
	}
	queryTokens := identityTokens(query)
	candidateTokens := identityTokens(name + " " + context)
	intersection := 0
	for token := range queryTokens {
		if candidateTokens[token] {
			intersection++
		}
	}
	if intersection > 0 {
		return 50 + 40*intersection/maxInt(len(queryTokens), 1)
	}
	return bigramDiceScore(needle, label)
}

func bigramDiceScore(left, right string) int {
	leftPairs, rightPairs := runePairs(left), runePairs(right)
	if len(leftPairs) == 0 || len(rightPairs) == 0 {
		return 0
	}
	counts := map[string]int{}
	for _, pair := range leftPairs {
		counts[pair]++
	}
	intersection := 0
	for _, pair := range rightPairs {
		if counts[pair] > 0 {
			intersection++
			counts[pair]--
		}
	}
	return 100 * 2 * intersection / (len(leftPairs) + len(rightPairs))
}

func runePairs(value string) []string {
	characters := []rune(value)
	if len(characters) == 1 {
		return []string{string(characters)}
	}
	result := make([]string, 0, len(characters)-1)
	for index := 0; index+1 < len(characters); index++ {
		result = append(result, string(characters[index:index+2]))
	}
	return result
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

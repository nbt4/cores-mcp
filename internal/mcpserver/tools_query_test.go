package mcpserver

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

func queryTestStore() *store.Store {
	return store.New(nil, time.Second, 200)
}

func TestRegisterQueryToolsBuildsInputSchemas(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "query-test", Version: "test"}, nil)
	registerQueryTools(server, queryTestStore())
}

func TestFlexibleQueryEntitiesAgainstDatabase(t *testing.T) {
	databaseURL := os.Getenv("CORES_MCP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CORES_MCP_TEST_DATABASE_URL is not set")
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := store.New(database, 10*time.Second, 200)
	ctx := context.Background()

	names := make([]string, 0, len(queryEntities))
	for name := range queryEntities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			spec := queryEntities[name]
			query, args, buildErr := buildEntityQuery(repository, spec, EntityQueryInput{Alias: "live", Entity: name, Limit: 1}, nil)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if _, queryErr := repository.Query(ctx, query, args...); queryErr != nil {
				t.Fatal(queryErr)
			}
		})
	}

	offers := queryEntities["procurement.offers"]
	query, args, err := buildAggregateQuery(repository, offers, QueryAggregateInput{Entity: "procurement.offers", GroupBy: []string{"currency"}, Metrics: []QueryAggregateMetric{{Function: "avg", Field: "price_per_unit_cents", Alias: "average_price"}}, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Query(ctx, query, args...); err != nil {
		t.Fatal(err)
	}

	_, _, _, err = executeRecordQueries(ctx, repository, QueryRecordsInput{
		Queries: []EntityQueryInput{
			{Alias: "jobs", Entity: "rental.jobs", Limit: 5},
			{Alias: "needs", Entity: "rental.requirements", Limit: 10},
			{Alias: "stock", Entity: "warehouse.products", Limit: 10},
			{Alias: "buy", Entity: "procurement.products", Limit: 10},
			{Alias: "offers", Entity: "procurement.offers", Limit: 10},
		},
		Joins: []QueryJoinInput{
			{Alias: "job_needs", Left: "jobs", Right: "needs", Relationship: "rental.job_requirements", Type: "left", Limit: 20},
			{Alias: "need_stock", Left: "needs", Right: "stock", Relationship: "inventory.requirement_product", Limit: 20},
			{Alias: "stock_buy", Left: "stock", Right: "buy", Relationship: "procurement.warehouse_product", Limit: 20},
			{Alias: "buy_offers", Left: "buy", Right: "offers", Relationship: "procurement.product_offers", Limit: 20},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQueryCatalogContainsCuratedCrossCoreEntities(t *testing.T) {
	catalog := queryCatalog()
	entities, ok := catalog["entities"].(map[string]any)
	if !ok {
		t.Fatal("catalog entities have unexpected type")
	}
	for _, name := range []string{"rental.jobs", "warehouse.products", "planner.tasks", "procurement.offers", "procurement.order_lines"} {
		if _, exists := entities[name]; !exists {
			t.Errorf("catalog is missing %s", name)
		}
	}
	serialized := strings.ToLower(prettyJSON(catalog))
	for _, forbidden := range []string{"password_hash", "api_token", "bank_account", "private_document"} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("catalog exposes forbidden field %s", forbidden)
		}
	}
}

func TestBuildEntityQueryUsesWhitelistedFieldsAndParameters(t *testing.T) {
	input := EntityQueryInput{
		Alias:  "jobs",
		Entity: "rental.jobs",
		Fields: []string{"job_id", "job_code"},
		Filters: []QueryFilterInput{
			{Field: "status", Operator: "eq", Value: "open' OR true --"},
			{Field: "start_date", Operator: "between", Values: []string{"2026-09-01", "2026-09-30"}},
		},
		Sort:  []QuerySortInput{{Field: "start_date", Direction: "desc", Nulls: "last"}},
		Limit: 25,
	}
	query, args, err := buildEntityQuery(queryTestStore(), queryEntities[input.Entity], input, map[string]bool{"customer_id": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "open' OR true") {
		t.Fatalf("filter value was interpolated into SQL: %s", query)
	}
	for _, expected := range []string{`q."job_id"`, `q."job_code"`, `q."customer_id"`, `q."status" = $1`, `q."start_date" BETWEEN $2 AND $3`, `LIMIT $4 OFFSET $5`} {
		if !strings.Contains(query, expected) {
			t.Errorf("query %q does not contain %q", query, expected)
		}
	}
	if len(args) != 5 || args[0] != "open' OR true --" || args[3] != 25 || args[4] != 0 {
		t.Fatalf("unexpected query args: %#v", args)
	}
}

func TestBuildEntityQueryRejectsUnknownFieldAndOperator(t *testing.T) {
	spec := queryEntities["warehouse.products"]
	_, _, err := buildEntityQuery(queryTestStore(), spec, EntityQueryInput{Fields: []string{"password_hash"}}, nil)
	if err == nil {
		t.Fatal("unknown selected field was accepted")
	}
	_, _, err = buildEntityQuery(queryTestStore(), spec, EntityQueryInput{Filters: []QueryFilterInput{{Field: "name", Operator: "raw_sql", Value: "1=1"}}}, nil)
	if err == nil {
		t.Fatal("unknown filter operator was accepted")
	}
}

func TestResolveQueryJoinSupportsCatalogRelationshipInBothDirections(t *testing.T) {
	queries := map[string]EntityQueryInput{
		"products": {Alias: "products", Entity: "warehouse.products"},
		"needs":    {Alias: "needs", Entity: "rental.requirements"},
	}
	forward, err := resolveQueryJoin(QueryJoinInput{Alias: "coverage", Left: "needs", Right: "products", Relationship: "inventory.requirement_product"}, queries)
	if err != nil {
		t.Fatal(err)
	}
	if forward.LeftField != "product_id" || forward.RightField != "product_id" {
		t.Fatalf("unexpected forward relationship: %#v", forward)
	}
	reverse, err := resolveQueryJoin(QueryJoinInput{Alias: "coverage", Left: "products", Right: "needs", Relationship: "inventory.requirement_product"}, queries)
	if err != nil {
		t.Fatal(err)
	}
	if reverse.LeftField != "product_id" || reverse.RightField != "product_id" {
		t.Fatalf("unexpected reverse relationship: %#v", reverse)
	}
}

func TestJoinQuerySectionsHonorsLeftJoinAndLimit(t *testing.T) {
	sections := map[string]querySection{
		"jobs":  {Rows: []map[string]any{{"job_id": int64(1), "name": "A"}, {"job_id": int64(2), "name": "B"}}},
		"tasks": {Rows: []map[string]any{{"job_id": int64(1), "task": "pick"}, {"job_id": int64(1), "task": "pack"}}},
	}
	joined, err := joinQuerySections(queryTestStore(), QueryJoinInput{Alias: "work", Left: "jobs", Right: "tasks", LeftField: "job_id", RightField: "job_id", Type: "left", Limit: 10}, sections)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Count != 3 || joined.Rows[2]["tasks"] != nil {
		t.Fatalf("unexpected left join result: %#v", joined.Rows)
	}
}

func TestBuildAggregateQueryValidatesMetrics(t *testing.T) {
	spec := queryEntities["procurement.offers"]
	input := QueryAggregateInput{
		Entity:  "procurement.offers",
		Filters: []QueryFilterInput{{Field: "active", Operator: "eq", Value: "true"}},
		GroupBy: []string{"supplier"},
		Metrics: []QueryAggregateMetric{{Function: "avg", Field: "price_per_unit_cents", Alias: "average_price"}, {Function: "count_distinct", Field: "procurement_product_id", Alias: "products"}},
		Sort:    []QuerySortInput{{Field: "average_price", Direction: "asc"}},
	}
	query, args, err := buildAggregateQuery(queryTestStore(), spec, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`AVG(q."price_per_unit_cents") AS "average_price"`, `COUNT(DISTINCT q."procurement_product_id") AS "products"`, `GROUP BY q."supplier"`, `ORDER BY "average_price" ASC`, `LIMIT $2`} {
		if !strings.Contains(query, expected) {
			t.Errorf("query %q does not contain %q", query, expected)
		}
	}
	if !reflect.DeepEqual(args, []any{true, 50}) {
		t.Fatalf("unexpected aggregate args: %#v", args)
	}

	_, _, err = buildAggregateQuery(queryTestStore(), spec, QueryAggregateInput{Metrics: []QueryAggregateMetric{{Function: "sum", Field: "supplier"}}})
	if err == nil {
		t.Fatal("sum over a string field was accepted")
	}
	_, _, err = buildAggregateQuery(queryTestStore(), spec, QueryAggregateInput{Metrics: []QueryAggregateMetric{{Function: "count", Field: "password_hash"}}})
	if err == nil {
		t.Fatal("count over an unknown field was accepted")
	}
}

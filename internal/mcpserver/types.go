package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type Source struct {
	Service string `json:"service"`
	Entity  string `json:"entity"`
	ID      string `json:"id,omitempty"`
}

type Output struct {
	AsOf     string   `json:"as_of" jsonschema:"UTC timestamp at which Cores produced the result"`
	Summary  string   `json:"summary" jsonschema:"Compact factual summary of the returned data"`
	Data     any      `json:"data" jsonschema:"Structured Cores records or metrics"`
	Sources  []Source `json:"sources,omitempty" jsonschema:"Suite services and records used for the result"`
	Warnings []string `json:"warnings,omitempty" jsonschema:"Data limitations or interpretation warnings"`
}

type SearchInput struct {
	Query  string `json:"query,omitempty" jsonschema:"Case-insensitive text to search for. Leave empty to list recent or relevant records."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum records to return; server limits always apply."`
	Offset int    `json:"offset,omitempty" jsonschema:"Number of matching records to skip."`
}

type IDInput struct {
	ID string `json:"id" jsonschema:"Cores record ID, code, barcode, serial number, or UUID."`
}

type WindowInput struct {
	From  string `json:"from,omitempty" jsonschema:"Start date in YYYY-MM-DD or RFC3339; defaults to today."`
	To    string `json:"to,omitempty" jsonschema:"End date in YYYY-MM-DD or RFC3339; defaults to 90 days after from."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum records to return; server limits always apply."`
}

type ProductWindowInput struct {
	Query              string  `json:"query" jsonschema:"Product name, code, barcode, model, manufacturer, or category."`
	From               string  `json:"from,omitempty" jsonschema:"Demand window start in YYYY-MM-DD or RFC3339; defaults to today."`
	To                 string  `json:"to,omitempty" jsonschema:"Demand window end in YYYY-MM-DD or RFC3339; defaults to 90 days after from."`
	SafetyStockPercent float64 `json:"safety_stock_percent,omitempty" jsonschema:"Additional safety stock percentage; defaults to 10 and is capped at 100."`
	Limit              int     `json:"limit,omitempty" jsonschema:"Maximum products to return."`
}

type SummaryInput struct {
	From string `json:"from,omitempty" jsonschema:"Start date in YYYY-MM-DD or RFC3339."`
	To   string `json:"to,omitempty" jsonschema:"End date in YYYY-MM-DD or RFC3339."`
}

type queryFn[In any] func(context.Context, In) (any, []Source, []string, error)

func addTool[In any](server *mcp.Server, name, title, description string, fn queryFn[In]) {
	closed := false
	mcp.AddTool(server, &mcp.Tool{
		Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Output, error) {
		data, sources, warnings, err := fn(ctx, input)
		if err != nil {
			message := fmt.Sprintf("%s failed: %v", name, err)
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
				AsOf: time.Now().UTC().Format(time.RFC3339), Summary: message, Warnings: []string{"No data was returned."},
			}, nil
		}
		output := Output{AsOf: time.Now().UTC().Format(time.RFC3339), Summary: summarize(data), Data: data, Sources: sources, Warnings: warnings}
		return nil, output, nil
	})
}

func rowsTool[In any](server *mcp.Server, db *store.Store, name, title, description, service, entity string, build func(In) (string, []any)) {
	addTool(server, name, title, description, func(ctx context.Context, input In) (any, []Source, []string, error) {
		query, args := build(input)
		rows, err := db.Query(ctx, query, args...)
		return rows, sourcesFor(service, entity, rows), nil, err
	})
}

func windowRowsTool(server *mcp.Server, db *store.Store, name, title, description, service, entity string, build func(WindowInput, time.Time, time.Time) (string, []any)) {
	addTool(server, name, title, description, func(ctx context.Context, input WindowInput) (any, []Source, []string, error) {
		from, to, err := dateWindow(input.From, input.To)
		if err != nil {
			return nil, nil, nil, err
		}
		query, args := build(input, from, to)
		rows, err := db.Query(ctx, query, args...)
		return rows, sourcesFor(service, entity, rows), nil, err
	})
}

func summaryRowsTool(server *mcp.Server, db *store.Store, name, title, description, service, entity string, build func(SummaryInput, time.Time, time.Time) (string, []any)) {
	addTool(server, name, title, description, func(ctx context.Context, input SummaryInput) (any, []Source, []string, error) {
		from, to, err := dateWindow(input.From, input.To)
		if err != nil {
			return nil, nil, nil, err
		}
		query, args := build(input, from, to)
		rows, err := db.Query(ctx, query, args...)
		return rows, sourcesFor(service, entity, rows), nil, err
	})
}

func sourcesFor(service, entity string, rows []map[string]any) []Source {
	result := make([]Source, 0, len(rows))
	seen := make(map[string]bool)
	for _, row := range rows {
		id := rowID(row)
		key := service + ":" + entity + ":" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Source{Service: service, Entity: entity, ID: id})
		if len(result) >= 50 {
			break
		}
	}
	if len(result) == 0 {
		return []Source{{Service: service, Entity: entity}}
	}
	return result
}

func rowID(row map[string]any) string {
	keys := []string{"id", "job_id", "jobid", "product_id", "productid", "device_id", "deviceid", "task_id", "plan_id", "case_id", "caseid", "order_id", "number", "code"}
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func summarize(data any) string {
	switch value := data.(type) {
	case []map[string]any:
		return fmt.Sprintf("Returned %d record(s).", len(value))
	case map[string]any:
		return fmt.Sprintf("Returned %d result section(s).", len(value))
	default:
		return "Cores data returned successfully."
	}
}

func searchPattern(value string) string { return "%" + strings.TrimSpace(value) + "%" }

func cleanOffset(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func dateWindow(fromRaw, toRaw string) (time.Time, time.Time, error) {
	from, err := parseDate(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: %w", err)
	}
	if from.IsZero() {
		now := time.Now()
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}
	to, err := parseDate(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: %w", err)
	}
	if to.IsZero() {
		to = from.AddDate(0, 0, 90)
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, errorsNew("to date must not be before from date")
	}
	if to.Sub(from) > 5*365*24*time.Hour {
		return time.Time{}, time.Time{}, errorsNew("date window may not exceed five years")
	}
	return from, to, nil
}

func parseDate(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339")
}

type stringError string

func (e stringError) Error() string { return string(e) }
func errorsNew(value string) error  { return stringError(value) }

func prettyJSON(value any) string {
	data, _ := json.MarshalIndent(value, "", "  ")
	return string(data)
}

func intID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 1 {
		return 0, errorsNew("id must be a positive integer")
	}
	return parsed, nil
}

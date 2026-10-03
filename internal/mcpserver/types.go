package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	coresauth "github.com/nbt4/cores-mcp/internal/authn"
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

func addCreateTool[In any](server *mcp.Server, name, title, description string, fn queryFn[In]) {
	closed, additive := false, false
	mcp.AddTool(server, &mcp.Tool{
		Name: name, Title: title, Description: mutationToolDescription(description),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &additive, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Output, error) {
		return executeMutationTool(ctx, name, "create", input, fn)
	})
}

func addUpdateTool[In any](server *mcp.Server, name, title, description string, fn queryFn[In]) {
	closed, destructive := false, true
	mcp.AddTool(server, &mcp.Tool{
		Name: name, Title: title, Description: mutationToolDescription(description),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Output, error) {
		return executeMutationTool(ctx, name, "write", input, fn)
	})
}

func mutationToolDescription(description string) string {
	return description + " Confirmed execution requires a unique idempotency_key. Set dry_run=true to validate and preview without changing data."
}

func executeMutationTool[In any](ctx context.Context, name, permissionLabel string, input In, fn queryFn[In]) (*mcp.CallToolResult, Output, error) {
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	var permissionErr error
	ctx, permissionErr = authorizeMutation(ctx, name)
	if permissionErr != nil {
		message := mutationPermissionMessage(name)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
			AsOf: now(), Summary: message, Warnings: []string{"No data was changed."},
		}, nil
	}

	if strings.HasPrefix(name, "warehouse.product_relations.") || name == "warehouse.products.link_relation" || strings.HasPrefix(name, "warehouse.categories.") || strings.HasPrefix(name, "warehouse.subcategories.") || strings.HasPrefix(name, "warehouse.third_categories.") || strings.HasPrefix(name, "warehouse.maintenance_orders.") || strings.HasPrefix(name, "warehouse.defects.") || strings.HasPrefix(name, "warehouse.tasks.") || strings.HasPrefix(name, "warehouse.inventory_counts.") {
		permissionErr = requireWarehouseMasterAdmin(ctx)
		raw, _ := json.Marshal(input)
		fields := map[string]any{}
		_ = json.Unmarshal(raw, &fields)
		financial := fields["cost_amount"] != nil
		if keys, ok := fields["clear_fields"].([]any); ok {
			for _, key := range keys {
				financial = financial || key == "cost_amount"
			}
		}
		if permissionErr == nil && financial {
			permissionErr = requireWarehouseFinancialScope(ctx)
		}
		if permissionErr != nil {
			message := permissionErr.Error()
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{AsOf: now(), Summary: message}, nil
		}
	}
	if isProcurementMasterLifecycleTool(name) || isProcurementWorkflowLifecycleTool(name) && strings.HasPrefix(name, "procurement.orders.") || name == "procurement.orders.receive" || isProcurementApprovalTool(name) || isProcurementOrderDraftTool(name) || isProcurementRequisitionOrderTool(name) || isProcurementSupplierSendTool(name) {
		if err := requireProcurementAdmin(ctx); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, Output{AsOf: now(), Summary: err.Error()}, nil
		}
	}
	if strings.HasPrefix(name, "rental.requirements.") || strings.HasPrefix(name, "rental.jobs.") || strings.HasPrefix(name, "rental.customers.") || strings.HasPrefix(name, "rental.venues.") {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, Output{AsOf: now(), Summary: err.Error()}, nil
		}
	}
	if strings.HasPrefix(name, "rental.jobs.") {
		if err := requireRentalJobFinancialInput(ctx, input); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, Output{AsOf: now(), Summary: err.Error()}, nil
		}
	}
	invocation, err := prepareWriteInvocation(ctx, name, input)
	if err != nil {
		message := fmt.Sprintf("%s rejected: %v", name, err)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
			AsOf: now(), Summary: message, Warnings: []string{"No data was changed."},
		}, nil
	}

	var replay *writeReplay
	if invocation.Confirmed {
		var owner bool
		replay, owner, err = mutationReplays.begin(ctx, invocation.ReplayKey, invocation.Fingerprint)
		if err != nil {
			message := fmt.Sprintf("%s rejected: %v", name, err)
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
				AsOf: now(), Summary: message, Warnings: []string{"No data was changed."},
			}, nil
		}
		if !owner {
			// Rental jobs/requirements authorize replay against current owner-side rights.
			// The durable receipt supplies the result without repeating the write.
			if isProcurementMasterLifecycleTool(name) || isProcurementWorkflowLifecycleTool(name) || name == "procurement.orders.receive" || isProcurementApprovalTool(name) || isProcurementRequisitionDraftTool(name) || isProcurementOrderDraftTool(name) || isProcurementRequisitionOrderTool(name) || isProcurementSupplierSendTool(name) || strings.HasPrefix(name, "rental.jobs.") || strings.HasPrefix(name, "rental.requirements.") {
				data, sources, warnings, err := fn(withMutationIdempotency(ctx, mutationControlsKey(input)), invocation.Input)
				if err != nil {
					message := fmt.Sprintf("%s failed: %v", name, err)
					if isProcurementSupplierSendTool(name) {
						warnings = append(warnings, mutationFailureWarning(name))
					}
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{AsOf: now(), Summary: message, Warnings: warnings}, nil
				}
				return nil, Output{AsOf: now(), Summary: summarize(data), Data: data, Sources: sources, Warnings: append(warnings, "Durable owner replay: current user rights rechecked; no additional mutation.")}, nil
			}
			warnings := append(append([]string(nil), replay.warnings...), "Idempotent replay: no additional mutation was executed.")
			if replay.err != nil {
				message := fmt.Sprintf("%s failed: %v", name, replay.err)
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
					AsOf: now(), Summary: message, Warnings: warnings,
				}, nil
			}
			return nil, Output{AsOf: now(), Summary: summarize(replay.data), Data: replay.data, Sources: replay.sources, Warnings: warnings}, nil
		}
		ctx = withMutationIdempotency(ctx, mutationControlsKey(input))
	}

	data, sources, warnings, err := fn(ctx, invocation.Input)
	if invocation.Confirmed {
		mutationReplays.finish(invocation.ReplayKey, replay, data, sources, warnings, err)
		if err != nil && hasDurableWarehouseRetry(name) {
			mutationReplays.forgetFailed(invocation.ReplayKey, replay)
		}
	}
	if err != nil {
		message := fmt.Sprintf("%s failed: %v", name, err)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{
			AsOf: now(), Summary: message, Warnings: append(warnings, mutationFailureWarning(name)),
		}, nil
	}
	if invocation.DryRun {
		data = map[string]any{"dry_run": true, "would_execute": data}
		warnings = append(warnings, "Dry-run only: no data was changed.")
	}
	return nil, Output{AsOf: now(), Summary: summarize(data), Data: data, Sources: sources, Warnings: warnings}, nil
}

func mutationControlsKey(input any) string {
	_, key := mutationControls(input)
	return strings.TrimSpace(key)
}

func mutationPermissionMessage(tool string) string {
	if isProcurementSupplierSendTool(tool) || tool == "warehouse.inventory_counts.approve" || tool == "procurement.orders.receive" || tool == "procurement.orders.transition" || tool == "procurement.requisitions.decide" {
		return tool + " requires the explicit " + requiredMutationScope(tool) + " scope and current administrator rights. Legacy cores:write, create and update access do not grant this workflow. Reconnect and explicitly grant the required scope."
	}
	return tool + " requires " + requiredMutationScope(tool) + " (or legacy cores:write). Reconnect the Cores MCP connector, choose Read and write (Lesen und Schreiben) in the Cores authorization dialog and confirm the selected access."
}

func authorizeMutation(ctx context.Context, tool string) (context.Context, error) {
	info := auth.TokenInfoFromContext(ctx)
	required := requiredMutationScope(tool)
	if info == nil || (isProcurementSupplierSendTool(tool) || tool == "warehouse.inventory_counts.approve" || tool == "procurement.orders.receive" || tool == "procurement.orders.transition" || tool == "procurement.requisitions.decide") && !containsString(info.Scopes, required) || (!containsString(info.Scopes, coresauth.WriteScope()) && !containsString(info.Scopes, required)) {
		return ctx, errorsNew("mutation scope is required")
	}
	return withMutationPermission(ctx, required), nil
}

func requiredMutationScope(tool string) string {
	if isProcurementMasterLifecycleTool(tool) || isProcurementWorkflowLifecycleTool(tool) {
		return coresauth.ServiceWriteScope("procurement", "archive")
	}
	if strings.HasPrefix(tool, "rental.requirements.") || strings.HasPrefix(tool, "rental.jobs.") || strings.HasPrefix(tool, "rental.customers.") || strings.HasPrefix(tool, "rental.venues.") {
		action := "update"
		if strings.HasSuffix(tool, ".create") {
			action = "create"
		}
		if strings.HasSuffix(tool, ".archive") || strings.HasSuffix(tool, ".restore") {
			action = "archive"
		}
		return coresauth.ServiceWriteScope("rental", action)
	}
	if strings.HasPrefix(tool, "warehouse.product_relations.") {
		action := "update"
		if strings.HasSuffix(tool, ".create") {
			action = "create"
		}
		if strings.HasSuffix(tool, ".archive") || strings.HasSuffix(tool, ".restore") {
			action = "archive"
		}
		return coresauth.ServiceWriteScope("warehouse", action)
	}
	if strings.HasPrefix(tool, "warehouse.inventory_counts.") {
		action := "update"
		if strings.HasSuffix(tool, ".create") {
			action = "create"
		}
		if strings.HasSuffix(tool, ".approve") {
			action = "approve"
		}
		if strings.HasSuffix(tool, ".archive") || strings.HasSuffix(tool, ".restore") {
			action = "archive"
		}
		return coresauth.ServiceWriteScope("warehouse", action)
	}
	if strings.HasPrefix(tool, "warehouse.maintenance_orders.") || strings.HasPrefix(tool, "warehouse.defects.") || strings.HasPrefix(tool, "warehouse.tasks.") {
		action := "update"
		if strings.HasSuffix(tool, ".create") {
			action = "create"
		}
		if strings.HasSuffix(tool, ".archive") || strings.HasSuffix(tool, ".restore") {
			action = "archive"
		}
		return coresauth.ServiceWriteScope("warehouse", action)
	}
	switch tool {
	case "rental.jobs.create", "rental.requirements.create":
		return coresauth.ServiceWriteScope("rental", "create")
	case "rental.jobs.assign_device", "rental.jobs.update", "rental.requirements.update":
		return coresauth.ServiceWriteScope("rental", "update")
	case "warehouse.maintenance_plans.create", "warehouse.cases.create", "warehouse.devices.create", "warehouse.devices.bulk_create", "warehouse.packages.create", "warehouse.tasks.create", "warehouse.products.create", "warehouse.manufacturers.create", "warehouse.brands.create", "warehouse.categories.create", "warehouse.subcategories.create", "warehouse.third_categories.create", "warehouse.locations.create":
		return coresauth.ServiceWriteScope("warehouse", "create")
	case "warehouse.maintenance_plans.update", "warehouse.cases.update", "warehouse.devices.update", "warehouse.devices.revert_update", "warehouse.packages.update", "warehouse.movements.create", "warehouse.devices.update_status", "warehouse.products.update", "warehouse.products.link_relation", "warehouse.locations.update", "warehouse.manufacturers.update", "warehouse.brands.update", "warehouse.categories.update", "warehouse.subcategories.update", "warehouse.third_categories.update":
		return coresauth.ServiceWriteScope("warehouse", "update")
	case "warehouse.categories.delete", "warehouse.subcategories.delete", "warehouse.third_categories.delete":
		return coresauth.ServiceWriteScope("warehouse", "delete")
	case "warehouse.categories.archive", "warehouse.categories.restore", "warehouse.subcategories.archive", "warehouse.subcategories.restore", "warehouse.third_categories.archive", "warehouse.third_categories.restore", "warehouse.maintenance_plans.archive", "warehouse.maintenance_plans.restore", "warehouse.manufacturers.archive", "warehouse.manufacturers.restore", "warehouse.brands.archive", "warehouse.brands.restore", "warehouse.cases.archive", "warehouse.cases.restore", "warehouse.locations.archive", "warehouse.locations.restore", "warehouse.packages.archive", "warehouse.packages.restore", "warehouse.devices.archive", "warehouse.devices.restore", "warehouse.products.archive", "warehouse.products.restore":
		return coresauth.ServiceWriteScope("warehouse", "archive")
	case "planner.plans.create", "planner.tasks.create":
		return coresauth.ServiceWriteScope("planner", "create")
	case "procurement.products.create", "procurement.offers.create", "procurement.orders.create", "procurement.suppliers.create", "procurement.categories.create", "procurement.requisitions.create", "procurement.requisitions.order":
		return coresauth.ServiceWriteScope("procurement", "create")
	case "procurement.suppliers.update", "procurement.categories.update", "procurement.products.update", "procurement.offers.update", "procurement.requisitions.update", "procurement.product_links.link", "procurement.orders.update":
		return coresauth.ServiceWriteScope("procurement", "update")
	case "procurement.requisitions.decide", "procurement.orders.transition":
		return coresauth.ServiceWriteScope("procurement", "approve")
	case "procurement.requisitions.submit":
		return coresauth.ServiceWriteScope("procurement", "submit")
	case "procurement.orders.send_amazon", "procurement.orders.reconcile_submission":
		return coresauth.ServiceWriteScope("procurement", "send")
	case "procurement.orders.receive":
		return coresauth.ServiceWriteScope("procurement", "receive")
	default:
		return coresauth.WriteScope()
	}
}

func executionToolForPreparation(name string) string {
	return strings.Replace(name, ".prepare_", ".", 1)
}

func addWritePreparationTool[In any](server *mcp.Server, name, title, description string, fn queryFn[In]) {
	closed := false
	mcp.AddTool(server, &mcp.Tool{
		Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Output, error) {
		executionTool := executionToolForPreparation(name)
		var permissionErr error
		ctx, permissionErr = authorizeMutation(ctx, executionTool)
		if permissionErr != nil {
			message := mutationPermissionMessage(executionTool)
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{AsOf: time.Now().UTC().Format(time.RFC3339), Summary: message}, nil
		}
		data, sources, warnings, err := fn(ctx, input)
		if err != nil {
			message := fmt.Sprintf("%s failed: %v", name, err)
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, Output{AsOf: time.Now().UTC().Format(time.RFC3339), Summary: message, Warnings: warnings}, nil
		}
		return nil, Output{AsOf: time.Now().UTC().Format(time.RFC3339), Summary: summarize(data), Data: data, Sources: sources, Warnings: warnings}, nil
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
	keys := []string{
		"id", "job_id", "jobid", "requirement_id", "customer_id", "venue_id",
		"product_id", "productid", "procurement_product_id", "device_id", "deviceid",
		"defect_id", "maintenance_id", "task_id", "plan_id", "case_id", "caseid",
		"offer_id", "supplier_id", "requisition_id", "requisition_line_id",
		"purchase_order_id", "order_id", "order_line_id", "number", "code",
	}
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

func mutationFailureWarning(name string) string {
	if isProcurementSupplierSendTool(name) {
		return "A durable supplier claim or external order may already exist. Retry only the original unchanged idempotency key to inspect or finalize its saved outcome. Never create a replacement order until the supplier outcome is reconciled."
	}
	return "No further data was changed."
}

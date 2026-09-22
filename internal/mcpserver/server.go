package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

const Version = "1.5.0"

func New(cfg config.Config, db *store.Store, logger *slog.Logger) *mcp.Server {
	description := "Read-only operational context and safe cross-core queries for RentalCore, WarehouseCore, PlannerCore and ProcurementCore."
	if cfg.EnableWrites {
		description = "Operational context, safe cross-core queries, and confirmed guided business workflows across all four Cores services."
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name: "cores-mcp", Title: "Cores Suite", Version: Version,
		Description: description,
		WebsiteURL:  cfg.PublicURL,
	}, nil)
	server.AddReceivingMiddleware(auditMiddleware(logger))
	registerSuiteTools(server, cfg, db)
	registerSchemaTools(server)
	registerQueryTools(server, db)
	registerRentalTools(server, db)
	registerWarehouseTools(server, db)
	registerWarehouseMasterTools(server, db)
	registerPlannerTools(server, db)
	registerProcurementTools(server, db)
	registerCrossCoreTools(server, db)
	if cfg.EnableWrites {
		registerCreateTools(server, cfg, db)
		registerWarehouseProductCreateTools(server, cfg, db)
		registerOperationalCreateTools(server, cfg, db)
		registerWorkflowTools(server, cfg, db)
		registerProcurementLifecycleTools(server, cfg, db)
	}
	registerKnowledge(server, cfg.KnowledgeDirs)
	registerPrompts(server)
	return server
}

func auditMiddleware(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			started := time.Now()
			result, err := next(ctx, method, request)
			attributes := []any{"method", method, "duration_ms", time.Since(started).Milliseconds()}
			if params, ok := request.GetParams().(*mcp.CallToolParamsRaw); ok {
				attributes = append(attributes, "tool", params.Name)
				if isMutationTool(params.Name) {
					attributes = append(attributes, mutationAuditAttributes(params.Arguments)...)
				}
			}
			if info := auth.TokenInfoFromContext(ctx); info != nil {
				attributes = append(attributes, "subject", info.UserID)
			}
			if err != nil {
				attributes = append(attributes, "error", err.Error())
				logger.Error("mcp request", attributes...)
			} else {
				if callResult, ok := result.(*mcp.CallToolResult); ok {
					attributes = append(attributes, "tool_error", callResult.IsError)
				}
				logger.Info("mcp request", attributes...)
			}
			return result, err
		}
	}
}

var mutationTools = map[string]struct{}{
	"planner.plans.create":            {},
	"planner.tasks.create":            {},
	"procurement.orders.create":       {},
	"procurement.products.create":     {},
	"procurement.requisitions.decide": {},
	"procurement.orders.receive":      {},
	"rental.jobs.assign_device":       {},
	"rental.jobs.create":              {},
	"rental.jobs.update":              {},
	"rental.requirements.create":      {},
	"rental.requirements.update":      {},
	"warehouse.devices.update_status": {},
	"warehouse.movements.create":      {},
	"warehouse.products.create":       {},
	"warehouse.tasks.create":          {},
}

func isMutationTool(name string) bool {
	_, ok := mutationTools[name]
	return ok
}

func mutationAuditAttributes(arguments json.RawMessage) []any {
	var values map[string]any
	if err := json.Unmarshal(arguments, &values); err != nil {
		return []any{"origin", "MCP/AI", "arguments_valid", false}
	}
	confirmed := false
	for name, value := range values {
		if len(name) > len("confirm_") && name[:len("confirm_")] == "confirm_" {
			if enabled, ok := value.(bool); ok && enabled {
				confirmed = true
			}
		}
	}
	dryRun, _ := values["dry_run"].(bool)
	attributes := []any{"origin", "MCP/AI", "dry_run", dryRun, "confirmed", confirmed}
	if key, ok := values["idempotency_key"].(string); ok && key != "" {
		digest := sha256.Sum256([]byte(key))
		attributes = append(attributes, "idempotency_ref", hex.EncodeToString(digest[:6]))
	}
	return attributes
}

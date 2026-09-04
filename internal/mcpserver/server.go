package mcpserver

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

const Version = "1.1.0"

func New(cfg config.Config, db *store.Store, logger *slog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name: "cores-mcp", Title: "Cores Suite", Version: Version,
		Description: "Read-only operational context and safe cross-core queries for RentalCore, WarehouseCore, PlannerCore and ProcurementCore.",
		WebsiteURL:  cfg.PublicURL,
	}, nil)
	server.AddReceivingMiddleware(auditMiddleware(logger))
	registerSuiteTools(server, cfg, db)
	registerQueryTools(server, db)
	registerRentalTools(server, db)
	registerWarehouseTools(server, db)
	registerPlannerTools(server, db)
	registerProcurementTools(server, db)
	registerCrossCoreTools(server, db)
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
			if info := auth.TokenInfoFromContext(ctx); info != nil {
				attributes = append(attributes, "subject", info.UserID)
			}
			if err != nil {
				attributes = append(attributes, "error", err.Error())
				logger.Error("mcp request", attributes...)
			} else {
				logger.Info("mcp request", attributes...)
			}
			return result, err
		}
	}
}

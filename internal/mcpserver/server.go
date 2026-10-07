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

const Version = "1.5.61"

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
	server.AddReceivingMiddleware(auditMiddleware(logger), toolOAuthMiddleware(cfg))
	registerSuiteTools(server, cfg, db)
	registerSchemaTools(server)
	registerQueryTools(server, db)
	registerRentalTools(server, db)
	registerRentalMasterReads(server, db)
	registerRentalJobAudit(server, db)
	registerRentalRequirementReads(server, db)
	registerRentalJobPositionReads(server, db)
	registerWarehouseTools(server, db)
	registerWarehouseAuditTool(server, db)
	registerWarehouseDeviceAuditTool(server, db)
	registerWarehouseMasterAuditTools(server, db)
	registerWarehouseCategoryAuditTools(server, db)
	registerWarehouseRelationReads(server, db)
	registerWarehouseMaintenancePlanReadTools(server, db)
	registerWarehouseMaintenanceOrderReadTools(server, db)
	registerWarehouseTaskReadTools(server, db)
	registerWarehouseInventoryReadTools(server, db)
	registerWarehousePackageAuditTool(server, db)
	registerWarehouseLocationAuditTool(server, db)
	registerWarehouseCaseReadTools(server, db)
	registerWarehouseCaseTemplateReads(server, db)
	registerWarehouseCaseContentReads(server, db)
	registerWarehouseCaseWorkflowReads(server, db)
	registerWarehouseMasterTools(server, db)
	registerMasterDataTools(server, db)
	registerPlannerTools(server, db)
	registerProcurementTools(server, db)
	registerProcurementAuditTool(server, db)
	registerProcurementMasterLifecycleTools(server, cfg, db)
	registerProcurementWorkflowLifecycleTools(server, cfg, db)
	registerCrossCoreTools(server, db)
	if cfg.EnableWrites {
		registerRentalMasterWrites(server, cfg)
		registerRentalJobLifecycle(server, cfg, db)
		registerRentalRequirementLifecycle(server, cfg, db)
		registerRentalJobPositionWrites(server, cfg, db)
		registerRentalJobExternalEquipmentWrites(server, cfg, db)
		registerCreateTools(server, cfg, db)
		registerSupplierCreateTools(server, cfg, db)
		registerSupplierUpdateTools(server, cfg, db)
		registerProcurementProductUpdateTools(server, cfg, db)
		registerProcurementOfferTools(server, cfg, db)
		registerProcurementRequisitionTools(server, cfg, db)
		registerProcurementOrderTransitionTools(server, cfg, db)
		registerProcurementOrderDraftTools(server, cfg, db)
		registerProcurementRequisitionOrderTools(server, cfg)
		registerProcurementAmazonSendTools(server, cfg)
		registerProcurementSubmissionReconcileTools(server, cfg)
		registerProcurementAdamHallTools(server, cfg)
		registerProcurementProductLinkTools(server, cfg, db)
		registerCategoryTools(server, cfg, db)
		registerWarehouseProductCreateTools(server, cfg, db)
		registerWarehouseProductUpdateTools(server, cfg, db)
		registerWarehouseProductLifecycleTools(server, cfg, db)
		registerWarehouseProductRelationTools(server, cfg, db)
		registerWarehouseMasterCreateTools(server, cfg, db)
		registerWarehouseCategoryCreateTools(server, cfg, db)
		registerWarehouseCategoryUpdateTools(server, cfg, db)
		registerWarehouseCategoryDeleteTools(server, cfg, db)
		registerWarehouseLocationCreateTools(server, cfg, db)
		registerWarehouseLocationUpdateTools(server, cfg, db)
		registerWarehouseLocationLifecycleTools(server, cfg, db)
		registerWarehousePackageTools(server, cfg, db)
		registerWarehousePackageLifecycleTools(server, cfg, db)
		registerWarehouseDeviceTools(server, cfg, db)
		registerWarehouseDeviceBulkTools(server, cfg)
		registerWarehouseProductImportTools(server, cfg)
		registerWarehouseCaseTools(server, cfg, db)
		registerWarehouseCaseTemplateTools(server, cfg)
		registerWarehouseCaseContentTools(server, cfg)
		registerWarehouseCaseWorkflowTools(server, cfg)
		registerWarehouseMasterUpdateTools(server, cfg, db)
		registerWarehouseMasterLifecycleTools(server, cfg)
		registerWarehouseCategoryLifecycleTools(server, cfg)
		registerWarehouseRelationTools(server, cfg)
		registerWarehouseMaintenancePlanTools(server, cfg)
		registerWarehouseMaintenanceOrderTools(server, cfg)
		registerWarehouseTaskTools(server, cfg)
		registerWarehouseInventoryTools(server, cfg)
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
	"rental.job_external_equipment.create":           {},
	"rental.job_positions.create":                    {},
	"rental.job_positions.update":                    {},
	"rental.job_positions.archive":                   {},
	"procurement.categories.archive":                 {},
	"procurement.categories.restore":                 {},
	"procurement.suppliers.archive":                  {},
	"procurement.suppliers.restore":                  {},
	"procurement.products.archive":                   {},
	"procurement.products.restore":                   {},
	"procurement.offers.archive":                     {},
	"procurement.offers.restore":                     {},
	"rental.requirements.archive":                    {},
	"rental.requirements.restore":                    {},
	"rental.jobs.archive":                            {},
	"rental.jobs.restore":                            {},
	"rental.customers.create":                        {},
	"rental.customers.update":                        {},
	"rental.customers.archive":                       {},
	"rental.customers.restore":                       {},
	"rental.customers.revert_update":                 {},
	"rental.venues.create":                           {},
	"rental.venues.update":                           {},
	"rental.venues.archive":                          {},
	"rental.venues.restore":                          {},
	"rental.venues.revert_update":                    {},
	"warehouse.inventory_counts.create":              {},
	"warehouse.inventory_counts.update":              {},
	"warehouse.inventory_counts.set_lines":           {},
	"warehouse.inventory_counts.review":              {},
	"warehouse.inventory_counts.return_for_counting": {},
	"warehouse.inventory_counts.approve":             {},
	"warehouse.inventory_counts.cancel":              {},
	"warehouse.inventory_counts.archive":             {},
	"warehouse.inventory_counts.restore":             {},
	"warehouse.tasks.update":                         {},
	"warehouse.tasks.start":                          {},
	"warehouse.tasks.complete":                       {},
	"warehouse.tasks.cancel":                         {},
	"warehouse.tasks.reopen":                         {},
	"warehouse.tasks.archive":                        {},
	"warehouse.tasks.restore":                        {},
	"warehouse.maintenance_orders.create":            {},
	"warehouse.maintenance_orders.update":            {},
	"warehouse.maintenance_orders.transition":        {},
	"warehouse.maintenance_orders.complete":          {},
	"warehouse.maintenance_orders.cancel":            {},
	"warehouse.maintenance_orders.reopen":            {},
	"warehouse.maintenance_orders.archive":           {},
	"warehouse.maintenance_orders.restore":           {},
	"warehouse.defects.create":                       {},
	"warehouse.defects.update":                       {},
	"warehouse.defects.transition":                   {},
	"warehouse.defects.complete":                     {},
	"warehouse.defects.cancel":                       {},
	"warehouse.defects.reopen":                       {},
	"warehouse.defects.archive":                      {},
	"warehouse.defects.restore":                      {},
	"warehouse.maintenance_plans.create":             {},
	"warehouse.maintenance_plans.update":             {},
	"warehouse.maintenance_plans.archive":            {},
	"warehouse.maintenance_plans.restore":            {},
	"warehouse.categories.archive":                   {},
	"warehouse.categories.restore":                   {},
	"warehouse.subcategories.archive":                {},
	"warehouse.subcategories.restore":                {},
	"warehouse.third_categories.archive":             {},
	"warehouse.third_categories.restore":             {},
	"warehouse.product_relations.create":             {},
	"warehouse.product_relations.update":             {},
	"warehouse.product_relations.archive":            {},
	"warehouse.product_relations.restore":            {},
	"warehouse.manufacturers.archive":                {}, "warehouse.manufacturers.restore": {}, "warehouse.brands.archive": {}, "warehouse.brands.restore": {},
	"warehouse.case_workflows.seal":           {},
	"warehouse.case_workflows.unseal":         {},
	"warehouse.case_workflows.move":           {},
	"warehouse.case_workflows.dispatch":       {},
	"warehouse.case_workflows.return":         {},
	"warehouse.case_workflows.inspect_return": {},
	"warehouse.case_contents.pack_device":     {},
	"warehouse.case_contents.pack_product":    {},
	"warehouse.case_contents.pack_case":       {},
	"warehouse.case_contents.unpack_device":   {},
	"warehouse.case_contents.unpack_product":  {},
	"warehouse.case_contents.unpack_case":     {},
	"warehouse.case_contents.unpack_all":      {},
	"warehouse.case_templates.create":         {},
	"warehouse.case_templates.update":         {},
	"warehouse.case_templates.archive":        {},
	"warehouse.case_templates.restore":        {},
	"warehouse.cases.create":                  {},
	"warehouse.cases.update":                  {},
	"warehouse.cases.archive":                 {},
	"warehouse.cases.restore":                 {},
	"warehouse.products.bulk_create":          {},
	"warehouse.devices.bulk_create":           {},
	"warehouse.devices.create":                {},
	"warehouse.devices.update":                {},
	"warehouse.devices.archive":               {},
	"warehouse.devices.restore":               {},
	"warehouse.devices.revert_update":         {},

	"planner.plans.create":              {},
	"planner.tasks.create":              {},
	"procurement.orders.create":         {},
	"procurement.products.create":       {},
	"procurement.products.update":       {},
	"procurement.offers.create":         {},
	"procurement.offers.update":         {},
	"procurement.suppliers.create":      {},
	"procurement.suppliers.update":      {},
	"procurement.categories.create":     {},
	"procurement.categories.update":     {},
	"procurement.requisitions.decide":   {},
	"procurement.requisitions.create":   {},
	"procurement.requisitions.update":   {},
	"procurement.requisitions.submit":   {},
	"procurement.orders.receive":        {},
	"procurement.orders.archive":        {},
	"procurement.orders.restore":        {},
	"procurement.requisitions.archive":  {},
	"procurement.requisitions.restore":  {},
	"procurement.orders.transition":     {},
	"procurement.orders.update":         {},
	"procurement.product_links.link":    {},
	"rental.jobs.assign_device":         {},
	"rental.jobs.create":                {},
	"rental.jobs.update":                {},
	"rental.requirements.create":        {},
	"rental.requirements.update":        {},
	"warehouse.devices.update_status":   {},
	"warehouse.movements.create":        {},
	"warehouse.products.create":         {},
	"warehouse.products.update":         {},
	"warehouse.products.archive":        {},
	"warehouse.products.restore":        {},
	"warehouse.products.link_relation":  {},
	"warehouse.manufacturers.create":    {},
	"warehouse.brands.create":           {},
	"warehouse.categories.create":       {},
	"warehouse.categories.update":       {},
	"warehouse.categories.delete":       {},
	"warehouse.subcategories.delete":    {},
	"warehouse.third_categories.delete": {},
	"warehouse.subcategories.update":    {},
	"warehouse.third_categories.update": {},
	"warehouse.subcategories.create":    {},
	"warehouse.third_categories.create": {},
	"warehouse.locations.create":        {},
	"warehouse.locations.update":        {},
	"warehouse.locations.archive":       {},
	"warehouse.locations.restore":       {},
	"warehouse.manufacturers.update":    {},
	"warehouse.brands.update":           {},
	"warehouse.packages.create":         {},
	"warehouse.packages.update":         {},
	"warehouse.packages.archive":        {},
	"warehouse.packages.restore":        {},
	"warehouse.tasks.create":            {},
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

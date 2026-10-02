package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type ProcurementMasterLifecycleInput struct {
	MutationControl
	ID                int64  `json:"id" jsonschema:"Exact existing ProcurementCore catalog record ID, including inactive records for restoration."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond version from the final owning-Core preview."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 binding retained business fields, parent records and dependencies from final preview."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record/context-bound lifecycle phrase from final preview."`
	ConfirmChange     bool   `json:"confirm_change,omitempty" jsonschema:"Set only after showing every retained field, dependency, effect and exact confirmation phrase and receiving explicit confirmation."`
}

type ProcurementMasterHistoryInput struct {
	ID    int64 `json:"id" jsonschema:"Exact ProcurementCore supplier, product, offer or category ID."`
	Limit int   `json:"limit,omitempty" jsonschema:"Maximum redacted events, capped at 100."`
}

func isProcurementMasterLifecycleTool(name string) bool {
	return (strings.HasPrefix(name, "procurement.categories.") || strings.HasPrefix(name, "procurement.suppliers.") || strings.HasPrefix(name, "procurement.products.") || strings.HasPrefix(name, "procurement.offers.")) && (strings.HasSuffix(name, ".archive") || strings.HasSuffix(name, ".restore"))
}

func invokeProcurementMasterLifecycle(ctx context.Context, cfg config.Config, namespace, operation string, input ProcurementMasterLifecycleInput, preview bool) (any, []Source, []string, error) {
	if err := requireProcurementAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	kind := map[string]string{"suppliers": "supplier", "products": "product", "offers": "offer", "categories": "category"}[namespace]
	sources := []Source{{Service: "procurementcore", Entity: kind, ID: fmt.Sprint(input.ID)}}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, sources, nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, sources, nil, err
	}
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || input.DryRun || !input.ConfirmChange
	result := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.ProcurementURL, "/api/v1/mcp/master-data/"+namespace+"/"+operation, http.MethodPost, body, &result)
	return result, sources, append(untrustedTextWarning(), "Archive/restore preserves IDs, original business fields and prices; no stock movements or external messages. Open orders/requisitions block affected archives; active products block category archives. Offer/product restoration requires active parents. Exact version/context, current administrator/archive rights and record-bound confirmation are required. Lifecycle, audit, native activity and durable receipt commit atomically; retries recheck current owning-Core rights."), err
}

func registerProcurementMasterLifecycleTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	for _, namespace := range []string{"suppliers", "products", "offers", "categories"} {
		ns := namespace
		for _, operation := range []string{"archive", "restore"} {
			if !cfg.EnableWrites {
				continue
			}
			op := operation
			addWritePreparationTool(server, "procurement."+ns+".prepare_"+op, "Prepare procurement "+ns+" "+op, "Review complete original fields, dependency/parent context, exact record version and lifecycle confirmation without changing business data.", func(ctx context.Context, input ProcurementMasterLifecycleInput) (any, []Source, []string, error) {
				return invokeProcurementMasterLifecycle(ctx, cfg, ns, op, input, true)
			})
			addUpdateTool(server, "procurement."+ns+"."+op, "Procurement "+ns+" "+op, "Execute the named lifecycle through the owner after final preview, exact version/context and explicit record-bound confirmation. Preserve original identity and fields with atomic audit/activity/durable replay.", func(ctx context.Context, input ProcurementMasterLifecycleInput) (any, []Source, []string, error) {
				return invokeProcurementMasterLifecycle(ctx, cfg, ns, op, input, false)
			})
		}
		addTool(server, "procurement."+ns+".audit_history", "Read procurement "+ns+" history", "Read bounded redacted action, actor, timestamp, result version and selected business changes for this exact record. Current procurement administrator required; raw audit JSON and private notes are excluded.", func(ctx context.Context, input ProcurementMasterHistoryInput) (any, []Source, []string, error) {
			return procurementAuditHistory(ctx, db, ProcurementAuditInput{Entity: map[string]string{"suppliers": "supplier", "products": "product", "offers": "offer", "categories": "category"}[ns], ID: input.ID, Limit: input.Limit})
		})
	}
}

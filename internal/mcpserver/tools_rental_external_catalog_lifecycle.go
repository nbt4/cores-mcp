package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RentalExternalCatalogControl struct {
	MutationControl
	EquipmentID       int64  `json:"equipment_id" jsonschema:"Exact external rental catalog ID, including inactive records. Never a warehouse product or device ID."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond catalog version from the final owner preview; required on execution."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact final context binding retained fields, supplier, duplicate candidates, custom fields and open job dependencies."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record and draft bound confirmation phrase returned by final preview."`
	ConfirmChange     bool   `json:"confirm_change,omitempty" jsonschema:"Only after reviewing full draft/diff/dependencies and explicit business confirmation."`
}

type RentalExternalCatalogUpdateInput struct {
	RentalExternalCatalogControl
	Name          *string  `json:"name,omitempty" jsonschema:"Nonempty product name, maximum 255 characters. Omit to preserve."`
	Category      *string  `json:"category,omitempty" jsonschema:"Nonempty category, maximum 100 characters."`
	Supplier      *string  `json:"supplier,omitempty" jsonschema:"Nonempty supplier display name, maximum 255 characters; must match a selected supplier_id."`
	SupplierID    *int64   `json:"supplier_id,omitempty" jsonschema:"Active Rental supplier ID. Explicit zero clears the optional linked supplier; omitted preserves it."`
	RentalPrice   *float64 `json:"rental_price,omitempty" jsonschema:"Future supplier rental price, 0–99999999.99 EUR with two decimals. Existing job cost snapshots remain unchanged."`
	CustomerPrice *float64 `json:"customer_price,omitempty" jsonschema:"Future customer rental price, 0–99999999.99 EUR with two decimals. Existing job sales prices remain unchanged."`
	Description   *string  `json:"description,omitempty" jsonschema:"Business description, maximum 10000 characters. Empty clears, omitted preserves."`
	Notes         *string  `json:"notes,omitempty" jsonschema:"Privileged catalog notes, maximum 10000 characters. Empty clears, omitted preserves."`
	AllowSimilar  bool     `json:"allow_similar,omitempty" jsonschema:"Only after explicitly reviewing similar identities. Exact name/supplier duplicates always block identity changes."`
}

type RentalExternalCatalogSearchInput struct {
	Query           string `json:"query,omitempty"`
	IncludeInactive bool   `json:"include_inactive,omitempty" jsonschema:"Explicitly include archived catalog identities for restoration. Active-only by default."`
	Limit           int    `json:"limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
}

func isRentalExternalCatalogMutation(name string) bool {
	return strings.HasPrefix(name, "rental.external_equipment.") && isMutationTool(name)
}

func invokeRentalExternalCatalogChange(ctx context.Context, cfg config.Config, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := requireRentalPositionFinancial(ctx); err != nil {
		return nil, nil, nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, nil, nil, err
	}
	body := map[string]any{}
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, nil, nil, err
	}
	dry, _ := body["dry_run"].(bool)
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	body["preview"] = preview || dry || body["confirm_change"] != true
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/external-equipment/"+op, http.MethodPost, body, &out)
	return out, []Source{{Service: "rentalcore", Entity: "rental_equipment", ID: fmt.Sprint(body["equipment_id"])}}, append(untrustedTextWarning(), "Catalog lifecycle retains identity, business fields, custom properties and all historical job cost/sales snapshots. Open jobs block archive and supplier/name changes. Restore the item before editing and restore any archived linked supplier first. Complete final version/context, selected action and separate rental financial rights, bound confirmation and durable owner audit/replay are required. No job assignment, stock or supplier message is changed."), err
}

const rentalExternalCatalogReadAccess = `EXISTS(SELECT 1 FROM users access_user WHERE access_user.userid::text=current_setting('cores.user_id',true) AND access_user.is_active AND access_user.is_admin)`
const rentalExternalCatalogReadColumns = `re.id AS equipment_id,re.name,re.supplier,re.supplier_id,re.category,re.description,re.rental_price,re.customer_price,re.is_active,to_char(re.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at`

func registerRentalExternalCatalogLifecycle(server *mcp.Server, cfg config.Config, db *store.Store) {
	if cfg.EnableWrites {
		addWritePreparationTool(server, "rental.external_equipment.prepare_update", "Prepare external catalog update", "Review retained catalog fields, exact diff, supplier and job dependencies. Future prices do not change job snapshots.", func(ctx context.Context, in RentalExternalCatalogUpdateInput) (any, []Source, []string, error) {
			return invokeRentalExternalCatalogChange(ctx, cfg, "update", in, true)
		})
		addUpdateTool(server, "rental.external_equipment.update", "Update external rental catalog item", "Apply the final confirmed partial catalog update through RentalCore with exact version/context and separate financial rights.", func(ctx context.Context, in RentalExternalCatalogUpdateInput) (any, []Source, []string, error) {
			return invokeRentalExternalCatalogChange(ctx, cfg, "update", in, false)
		})
		for _, operation := range []string{"archive", "restore"} {
			op := operation
			addWritePreparationTool(server, "rental.external_equipment.prepare_"+op, "Prepare external catalog "+op, "Review retained identity and prices, open job blockers and exact lifecycle confirmation. No business mutation.", func(ctx context.Context, in RentalExternalCatalogControl) (any, []Source, []string, error) {
				return invokeRentalExternalCatalogChange(ctx, cfg, op, in, true)
			})
			addUpdateTool(server, "rental.external_equipment."+op, "External rental catalog "+op, "Perform the named retained lifecycle with administrator/archive plus financial rights, final context and explicit record-bound confirmation.", func(ctx context.Context, in RentalExternalCatalogControl) (any, []Source, []string, error) {
				return invokeRentalExternalCatalogChange(ctx, cfg, op, in, false)
			})
		}
	}
	addTool(server, "rental.external_equipment.search", "Search external rental catalog", "Search bounded canonical names, suppliers and categories; explicitly include inactive records for restoration. Notes and raw audit contents are excluded; prices require separate rental financial access. Current Rental administrator required.", func(ctx context.Context, in RentalExternalCatalogSearchInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT `+rentalExternalCatalogReadColumns+` FROM rental_equipment re WHERE `+rentalExternalCatalogReadAccess+` AND ($1 OR re.is_active) AND ($2='' OR re.name ILIKE $3 OR re.supplier ILIKE $3 OR re.category ILIKE $3) ORDER BY re.name,re.id LIMIT $4 OFFSET $5`, in.IncludeInactive, strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit), cleanOffset(in.Offset))
		sources := []Source{}
		for _, row := range rows {
			sources = append(sources, Source{Service: "rentalcore", Entity: "rental_equipment", ID: fmt.Sprint(row["equipment_id"])})
		}
		return rows, sources, untrustedTextWarning(), err
	})
	addTool(server, "rental.external_equipment.get", "Get external rental catalog item", "Read one exact active or archived catalog item without notes. Prices require separate financial access; current Rental administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		id, err := rentalExternalCatalogReadID(in.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT `+rentalExternalCatalogReadColumns+` FROM rental_equipment re WHERE re.id=$1 AND `+rentalExternalCatalogReadAccess, id)
		return rows, []Source{{Service: "rentalcore", Entity: "rental_equipment", ID: fmt.Sprint(id)}}, untrustedTextWarning(), err
	})
	addTool(server, "rental.external_equipment.audit_history", "Read external catalog history", "Read bounded redacted owner/native catalog changes, actor, timestamp, exact result version, identity and active state. Financial before/after values require rental financial scope; notes, descriptions, raw JSON and unrelated employee data are excluded. Current administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
		if err := requireRentalMasterAdmin(ctx); err != nil {
			return nil, nil, nil, err
		}
		id, err := rentalExternalCatalogReadID(in.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,COALESCE(new_values->>'origin','UI') AS origin,new_values#>>'{after,updated_at}' AS result_version,old_values->>'is_active' AS active_before,new_values#>>'{after,is_active}' AS active_after,old_values->>'name' AS name_before,new_values#>>'{after,name}' AS name_after,old_values->>'rental_price' AS rental_price_before,new_values#>>'{after,rental_price}' AS rental_price_after,old_values->>'customer_price' AS customer_price_before,new_values#>>'{after,customer_price}' AS customer_price_after FROM audit_log WHERE entity_type IN ('rental_external_equipment','rental_equipment') AND entity_id=$1 AND `+rentalExternalCatalogReadAccess+` ORDER BY id DESC LIMIT 100`, fmt.Sprint(id))
		return rows, []Source{{Service: "rentalcore", Entity: "rental_equipment", ID: fmt.Sprint(id)}}, nil, err
	})
}

func rentalExternalCatalogReadID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("Exact positive external rental catalog ID required")
	}
	return id, nil
}

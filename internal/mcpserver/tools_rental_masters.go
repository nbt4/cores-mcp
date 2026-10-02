package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type RentalMasterControl struct {
	MutationControl
	ID                int64  `json:"id,omitempty" jsonschema:"Exact existing customer or venue ID for update/archive/restore; omit for create."`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"Exact microsecond version from the final owner preview, required for confirmed existing records."`
	ExpectedContext   string `json:"expected_context,omitempty" jsonschema:"Exact SHA-256 preview binding all current/draft fields, duplicate candidates and active job versions."`
	AllowDuplicate    bool   `json:"allow_duplicate,omitempty" jsonschema:"Set only after reviewing matching active records and explicitly confirming a distinct business identity. Archived exact matches must be restored."`
	ConfirmChange     bool   `json:"confirm_change,omitempty" jsonschema:"Set only after presenting all business fields, diff, matching records and dependencies and receiving explicit confirmation."`
	ConfirmationText  string `json:"confirmation_text,omitempty" jsonschema:"Exact record/draft-bound CREATE/UPDATE/ARCHIVE/RESTORE phrase from the final preview."`
}
type RentalCustomerInput struct {
	RentalMasterControl
	Name         *string `json:"name,omitempty" jsonschema:"Optional legacy display name, maximum 255 characters."`
	CompanyName  *string `json:"company_name,omitempty" jsonschema:"Company name, maximum 255. Identity requires company/name or first/last name."`
	FirstName    *string `json:"first_name,omitempty" jsonschema:"Business contact first name, maximum 100."`
	LastName     *string `json:"last_name,omitempty" jsonschema:"Business contact last name, maximum 100."`
	Street       *string `json:"street,omitempty" jsonschema:"Business address street, maximum 255."`
	HouseNumber  *string `json:"house_number,omitempty" jsonschema:"Maximum 20 characters."`
	ZIP          *string `json:"zip,omitempty" jsonschema:"Postal code, maximum 20; preserve leading zeroes."`
	City         *string `json:"city,omitempty" jsonschema:"Maximum 100 characters."`
	FederalState *string `json:"federal_state,omitempty" jsonschema:"Maximum 100 characters."`
	Country      *string `json:"country,omitempty" jsonschema:"Maximum 100 characters."`
	Phone        *string `json:"phone,omitempty" jsonschema:"Business phone, maximum 50 characters."`
	Email        *string `json:"email,omitempty" jsonschema:"Plain valid business email address, maximum 255."`
	CustomerType *string `json:"customer_type,omitempty" jsonschema:"Unternehmen, Privat or empty."`
	IsCustomer   *bool   `json:"is_customer,omitempty" jsonschema:"Customer role; defaults to true at create."`
	IsSupplier   *bool   `json:"is_supplier,omitempty" jsonschema:"Supplier role; defaults to false. At least one role is required."`
	Notes        *string `json:"notes,omitempty" jsonschema:"Business notes, maximum 10000. All string fields preserve omission; explicit empty clears to null."`
}
type RentalVenueInput struct {
	RentalMasterControl
	Name        *string `json:"name,omitempty" jsonschema:"Required venue name, maximum 255 characters."`
	Street      *string `json:"street,omitempty" jsonschema:"Venue street, maximum 255."`
	HouseNumber *string `json:"house_number,omitempty" jsonschema:"Maximum 50 characters."`
	ZIP         *string `json:"zip,omitempty" jsonschema:"Postal code, maximum 20; preserve leading zeroes."`
	City        *string `json:"city,omitempty" jsonschema:"Maximum 255 characters."`
	ContactName *string `json:"contact_name,omitempty" jsonschema:"Business contact name, maximum 255."`
	Phone       *string `json:"phone,omitempty" jsonschema:"Business phone, maximum 100."`
	Email       *string `json:"email,omitempty" jsonschema:"Plain valid business email address, maximum 255."`
	Notes       *string `json:"notes,omitempty" jsonschema:"Business notes, maximum 10000. Optional string fields preserve omission; explicit empty clears to null."`
}

func requireRentalMasterAdmin(ctx context.Context) error {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return fmt.Errorf("Rental administrator permission is required")
	}
	id, err := strconv.ParseUint(info.UserID, 10, 32)
	if err != nil || id == 0 {
		return fmt.Errorf("A real signed-in Rental user is required")
	}
	return nil
}
func invokeRentalMaster(ctx context.Context, cfg config.Config, entity, op string, input any, preview bool) (any, []Source, []string, error) {
	if err := requireRentalMasterAdmin(ctx); err != nil {
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
	dryRun, _ := body["dry_run"].(bool)
	delete(body, "dry_run")
	delete(body, "idempotency_key")
	confirmed, _ := body["confirm_change"].(bool)
	body["preview"] = preview || dryRun || !confirmed
	out := map[string]any{}
	err = newCoreAPIClient(cfg).doJSON(ctx, cfg.RentalURL, "/api/v1/mcp/"+entity+"/"+op, http.MethodPost, body, &out)
	id := ""
	for _, key := range []string{"record", "current", "draft"} {
		if record, ok := out[key].(map[string]any); ok && record["id"] != nil {
			id = fmt.Sprint(record["id"])
			break
		}
	}
	kind := strings.TrimSuffix(entity, "s")
	return out, []Source{{Service: "rentalcore", Entity: kind, ID: id}}, []string{"Preparation requires current administrator/action rights and returns complete business fields, including contact details and notes. Default reads and histories redact these fields. Archive retains identity and history and blocks active jobs; restore preserves every business field. No external messages are sent. Record, audit and durable receipt commit atomically."}, err
}
func registerRentalMasterWrites(server *mcp.Server, cfg config.Config) {
	for _, op := range []string{"create", "update", "archive", "restore"} {
		operation := op
		addWritePreparationTool(server, "rental.customers.prepare_"+operation, "Prepare customer "+operation, "Review all customer fields, duplicate candidates, exact record/context versions and active jobs without changing data.", func(ctx context.Context, in RentalCustomerInput) (any, []Source, []string, error) {
			return invokeRentalMaster(ctx, cfg, "customers", operation, in, true)
		})
		customer := func(ctx context.Context, in RentalCustomerInput) (any, []Source, []string, error) {
			return invokeRentalMaster(ctx, cfg, "customers", operation, in, false)
		}
		addWritePreparationTool(server, "rental.venues.prepare_"+operation, "Prepare venue "+operation, "Review all venue fields, duplicate candidates, exact record/context versions and active jobs without changing data.", func(ctx context.Context, in RentalVenueInput) (any, []Source, []string, error) {
			return invokeRentalMaster(ctx, cfg, "venues", operation, in, true)
		})
		venue := func(ctx context.Context, in RentalVenueInput) (any, []Source, []string, error) {
			return invokeRentalMaster(ctx, cfg, "venues", operation, in, false)
		}
		if operation == "create" {
			addCreateTool(server, "rental.customers.create", "Create customer", "Create one explicitly confirmed customer through the Rental owner, with complete preview and atomic audit/replay.", customer)
			addCreateTool(server, "rental.venues.create", "Create venue", "Create one explicitly confirmed venue through the Rental owner, with complete preview and atomic audit/replay.", venue)
		} else {
			addUpdateTool(server, "rental.customers."+operation, "Customer "+operation, "Execute the named versioned customer workflow after elevated confirmation. Lifecycle preserves metadata and history.", customer)
			addUpdateTool(server, "rental.venues."+operation, "Venue "+operation, "Execute the named versioned venue workflow after elevated confirmation. Lifecycle preserves metadata and history.", venue)
		}
	}
}
func registerRentalMasterReads(server *mcp.Server, db *store.Store) {
	for _, kind := range []string{"customer", "venue"} {
		entity := kind
		namespace := "rental." + kind + "s"
		base := `SELECT c.customerid AS id,c.customerid AS customer_id,COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),trim(concat_ws(' ',c.firstname,c.lastname))) AS name,c.customertype AS customer_type,c.city,c.country,c.is_customer,c.is_supplier,COALESCE(c.is_archived,false) AS is_archived,c.archived_at,to_char(c.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM customers c`
		if entity == "venue" {
			base = `SELECT v.id AS id,v.id AS venue_id,v.name,v.city,v.zip,v.is_archived,v.archived_at,to_char(v.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at FROM venues v`
		}
		queryBase := base
		addTool(server, namespace+".get", "Get "+kind+" identity", "Read minimal business identity, lifecycle and exact version including archived records. Contact details and notes are excluded.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
			rows, err := db.Query(ctx, "SELECT * FROM ("+queryBase+") r WHERE r.id::text=$1", strings.TrimSpace(in.ID))
			return rows, []Source{{Service: "rentalcore", Entity: entity, ID: in.ID}}, nil, err
		})
		addTool(server, namespace+".resolve", "Resolve "+kind, "Find bounded identity candidates including archives. Exact active matches are selected; exact archived matches require restore and are never recreated automatically.", func(ctx context.Context, in SearchInput) (any, []Source, []string, error) {
			rows, err := db.Query(ctx, "SELECT * FROM ("+queryBase+") r WHERE r.id::text=$1 OR r.name ILIKE $2 OR r.city ILIKE $2 ORDER BY (lower(r.name)=lower($1)) DESC,r.id LIMIT $3", strings.TrimSpace(in.Query), searchPattern(in.Query), db.Limit(in.Limit))
			if err != nil {
				return nil, nil, nil, err
			}
			pool, err := db.Query(ctx, "SELECT * FROM ("+queryBase+") r ORDER BY r.id LIMIT $1", db.Limit(500))
			if err != nil {
				return nil, nil, nil, err
			}
			seen := map[string]bool{}
			for _, r := range rows {
				seen[fmt.Sprint(r["id"])] = true
			}
			for _, r := range rankMasterCandidates(pool, in.Query, in.Limit) {
				if !seen[fmt.Sprint(r["id"])] {
					rows = append(rows, r)
					seen[fmt.Sprint(r["id"])] = true
				}
			}
			status := "needs_selection"
			var selected any
			exact := []map[string]any{}
			for _, r := range rows {
				if fmt.Sprint(r["id"]) == strings.TrimSpace(in.Query) || strings.EqualFold(fmt.Sprint(r["name"]), strings.TrimSpace(in.Query)) {
					exact = append(exact, r)
				}
			}
			if len(exact) == 1 {
				if exact[0]["is_archived"] == true {
					status = "restoration_required"
				} else {
					status = "resolved"
					selected = exact[0]
				}
			}
			warnings := []string{}
			if selected == nil && status != "restoration_required" && len(pool) >= db.Limit(500) {
				status = "incomplete"
				warnings = append(warnings, "Fuzzy candidate pool reached the bounded row limit; narrow the query or use an exact ID. No automatic creation or selection is authorized.")
			}
			return map[string]any{"resolution_status": status, "selected": selected, "candidates": rows}, sourcesFor("rentalcore", entity, rows), warnings, nil
		})
		addTool(server, namespace+".audit_history", "Read "+kind+" history", "Read redacted per-record action/actor/time, result versions and lifecycle changes. Business contacts, addresses, notes, raw audit data and external sync IDs are excluded. Rental administrator required.", func(ctx context.Context, in IDInput) (any, []Source, []string, error) {
			if err := requireRentalMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			rows, err := db.Query(ctx, `SELECT id AS audit_id,user_id,timestamp,action,new_values->>'origin' AS origin,new_values->>'updated_at' AS result_version,old_values->>'is_archived' AS archived_before,new_values#>>'{after,is_archived}' AS archived_after FROM audit_log WHERE entity_type=$1 AND entity_id=$2 ORDER BY id DESC LIMIT 100`, "rental_"+entity, strings.TrimSpace(in.ID))
			return rows, []Source{{Service: "rentalcore", Entity: entity, ID: in.ID}}, nil, err
		})
	}
}

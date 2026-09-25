package mcpserver

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type JobDeviceAssignInput struct {
	MutationControl
	JobID             int64   `json:"job_id,omitempty" jsonschema:"Exact existing RentalCore job ID."`
	JobQuery          string  `json:"job_query,omitempty" jsonschema:"Job code or description to resolve when job_id is unknown."`
	DeviceID          string  `json:"device_id,omitempty" jsonschema:"Exact active device ID, barcode, QR code, or serial number."`
	Price             float64 `json:"price,omitempty" jsonschema:"Optional rental price for this device assignment; must not be negative."`
	ConfirmAssignment bool    `json:"confirm_assignment,omitempty" jsonschema:"Set true only after showing the final assignment preview and receiving explicit confirmation."`
}

type JobUpdateInput struct {
	MutationControl
	JobID         int64    `json:"job_id,omitempty" jsonschema:"Exact existing RentalCore job ID."`
	JobQuery      string   `json:"job_query,omitempty" jsonschema:"Job code or description to resolve when job_id is unknown."`
	Description   string   `json:"description,omitempty"`
	CustomerID    int64    `json:"customer_id,omitempty"`
	StatusID      int64    `json:"status_id,omitempty" jsonschema:"Existing RentalCore status ID, including the configured cancelled status when cancelling a job."`
	StatusQuery   string   `json:"status_query,omitempty"`
	StartDate     string   `json:"start_date,omitempty" jsonschema:"New date in YYYY-MM-DD; omitted values remain unchanged."`
	EndDate       string   `json:"end_date,omitempty" jsonschema:"New date in YYYY-MM-DD; omitted values remain unchanged."`
	VenueID       *int64   `json:"venue_id,omitempty" jsonschema:"New venue ID. Use 0 to clear the venue; omit to keep it unchanged."`
	Revenue       *float64 `json:"revenue,omitempty" jsonschema:"New planned revenue; omit to keep it unchanged."`
	ConfirmUpdate bool     `json:"confirm_update,omitempty" jsonschema:"Set true only after showing the current values and final update preview and receiving explicit confirmation."`
}

type RequirementUpdateInput struct {
	MutationControl
	RequirementID int64 `json:"requirement_id,omitempty" jsonschema:"Exact RentalCore job requirement ID."`
	Quantity      int   `json:"quantity,omitempty" jsonschema:"Required new positive quantity."`
	ConfirmUpdate bool  `json:"confirm_update,omitempty" jsonschema:"Set true only after showing the old and new quantity and receiving explicit confirmation."`
}

type PurchaseOrderLineInput struct {
	ProductID      int64   `json:"product_id,omitempty" jsonschema:"Optional active ProcurementCore product ID."`
	Description    string  `json:"description,omitempty" jsonschema:"Required when product_id is omitted; otherwise defaults to the product name."`
	Quantity       float64 `json:"quantity,omitempty" jsonschema:"Required positive quantity."`
	Unit           string  `json:"unit,omitempty" jsonschema:"Defaults to Stk."`
	UnitPriceCents int64   `json:"unit_price_cents,omitempty" jsonschema:"Non-negative unit price in integer cents."`
	PurchaseURL    string  `json:"purchase_url,omitempty"`
}

type PurchaseOrderCreateInput struct {
	MutationControl
	SupplierID          int64                    `json:"supplier_id,omitempty" jsonschema:"Exact active ProcurementCore supplier ID."`
	SupplierQuery       string                   `json:"supplier_query,omitempty" jsonschema:"Supplier name or code to resolve when supplier_id is unknown."`
	SupplierOrderNumber string                   `json:"supplier_order_number,omitempty"`
	Status              string                   `json:"status,omitempty" jsonschema:"MCP creation is draft only; defaults to draft. Use procurement.orders.prepare_transition for later status changes."`
	Currency            string                   `json:"currency,omitempty" jsonschema:"One of EUR, CHF, USD, or GBP; defaults to EUR."`
	OrderDate           string                   `json:"order_date,omitempty" jsonschema:"Optional date in YYYY-MM-DD."`
	ExpectedDelivery    string                   `json:"expected_delivery,omitempty" jsonschema:"Optional date in YYYY-MM-DD."`
	Notes               string                   `json:"notes,omitempty"`
	Lines               []PurchaseOrderLineInput `json:"lines,omitempty" jsonschema:"At least one validated order line."`
	ConfirmCreation     bool                     `json:"confirm_creation,omitempty" jsonschema:"Set true only after showing supplier, every line, totals and dates and receiving explicit confirmation."`
}

type WarehouseMovementCreateInput struct {
	MutationControl
	ScanCode        string   `json:"scan_code,omitempty" jsonschema:"Exact device ID, barcode, QR code, or quantity-item barcode."`
	Action          string   `json:"action,omitempty" jsonschema:"One of intake, outtake, or transfer."`
	JobID           *int64   `json:"job_id,omitempty" jsonschema:"Required for outtake."`
	ZoneID          *int64   `json:"zone_id,omitempty" jsonschema:"Required for intake and transfer."`
	Quantity        *float64 `json:"quantity,omitempty" jsonschema:"Positive quantity for a quantity-tracked item."`
	Notes           string   `json:"notes,omitempty"`
	ConfirmCreation bool     `json:"confirm_creation,omitempty" jsonschema:"Set true only after showing the physical movement preview and receiving explicit confirmation."`
}

type DeviceStatusUpdateInput struct {
	MutationControl
	DeviceID        string `json:"device_id,omitempty" jsonschema:"Exact active WarehouseCore device ID."`
	Status          string `json:"status,omitempty" jsonschema:"Optional physical status: in_storage or location_unknown. Physical job movement states must use warehouse.movements.create."`
	ConditionStatus string `json:"condition_status,omitempty" jsonschema:"Optional condition: available, blocked, defective, maintenance, or retired."`
	ConfirmUpdate   bool   `json:"confirm_update,omitempty" jsonschema:"Set true only after showing current and new states and receiving explicit confirmation."`
}

type preparedMutation struct {
	Draft          map[string]any            `json:"draft"`
	Current        map[string]any            `json:"current,omitempty"`
	Diff           map[string]map[string]any `json:"diff,omitempty"`
	RelatedRecords []map[string]any          `json:"related_records,omitempty"`
	Missing        []string                  `json:"required_missing_fields"`
	Questions      []map[string]any          `json:"questions_for_user"`
	Warnings       []string                  `json:"risk_warnings,omitempty"`
	Ready          bool                      `json:"ready_to_execute"`
}

func (p preparedMutation) response(status string) map[string]any {
	result := map[string]any{
		"operation_status": status, "ready_to_execute": p.Ready, "draft": p.Draft, "current": p.Current,
		"required_missing_fields": p.Missing, "questions_for_user": p.Questions, "risk_warnings": p.Warnings,
	}
	if len(p.Diff) > 0 {
		result["diff"] = p.Diff
	}
	if len(p.RelatedRecords) > 0 {
		result["related_records"] = p.RelatedRecords
	}
	return result
}

func (p *preparedMutation) require(field, prompt string, options any) {
	if containsString(p.Missing, field) {
		return
	}
	p.Missing = append(p.Missing, field)
	p.Questions = append(p.Questions, question(field, prompt, "required", options))
}

func (p *preparedMutation) finish() {
	p.Missing = unique(p.Missing)
	p.Ready = len(p.Missing) == 0
}

func registerWorkflowTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)

	addWritePreparationTool(server, "rental.jobs.prepare_assign_device", "Prepare device assignment", "Resolve a live job and active serialized device, check existing and overlapping assignments, and preview the exact assignment.", func(ctx context.Context, input JobDeviceAssignInput) (any, []Source, []string, error) {
		prepared, err := prepareJobDeviceAssign(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "rentalcore", "job_device_assignment_draft"), prepared.Warnings, err
	})
	addCreateTool(server, "rental.jobs.assign_device", "Assign device to rental job", "Assign one validated serialized device to one RentalCore job. First call rental.jobs.prepare_assign_device, show the preview, and obtain explicit confirmation.", func(ctx context.Context, input JobDeviceAssignInput) (any, []Source, []string, error) {
		prepared, err := prepareJobDeviceAssign(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "rentalcore", "job_device_assignment_draft"), prepared.Warnings, err
		}
		if !input.ConfirmAssignment {
			return prepared.response("confirmation_required"), mutationSources(prepared, "rentalcore", "job_device_assignment_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		jobID, deviceID := numericID(prepared.Draft["job_id"]), fmt.Sprint(prepared.Draft["device_id"])
		var result map[string]any
		path := fmt.Sprintf("/api/v1/jobs/%d/devices/%s", jobID, url.PathEscape(deviceID))
		if err := api.doJSON(ctx, cfg.RentalURL, path, http.MethodPost, map[string]any{"price": input.Price}, &result); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "assigned", "assignment": prepared.Draft, "result": result}, []Source{{Service: "rentalcore", Entity: "job", ID: fmt.Sprint(jobID)}, {Service: "warehousecore", Entity: "device", ID: deviceID}}, prepared.Warnings, nil
	})

	addWritePreparationTool(server, "rental.jobs.prepare_update", "Prepare rental job update", "Load the current job, resolve changed references and statuses, validate dates, and preview every resulting value. A cancellation is a status update to the configured cancelled status.", func(ctx context.Context, input JobUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareJobUpdate(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "rentalcore", "job_update_draft"), prepared.Warnings, err
	})
	addUpdateTool(server, "rental.jobs.update", "Update or cancel rental job", "Update one RentalCore job after comparing current and proposed values. First call rental.jobs.prepare_update and obtain explicit confirmation. Use the configured cancelled status instead of deleting jobs.", func(ctx context.Context, input JobUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareJobUpdate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "rentalcore", "job_update_draft"), prepared.Warnings, err
		}
		if !input.ConfirmUpdate {
			return prepared.response("confirmation_required"), mutationSources(prepared, "rentalcore", "job_update_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		jobID := numericID(prepared.Draft["job_id"])
		payload := cloneWithout(prepared.Draft, "job_id", "job", "status", "customer", "venue")
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.RentalURL, fmt.Sprintf("/api/v1/jobs/%d", jobID), http.MethodPut, payload, &updated); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "updated", "previous": prepared.Current, "job": updated}, []Source{{Service: "rentalcore", Entity: "job", ID: fmt.Sprint(jobID)}}, prepared.Warnings, nil
	})

	addWritePreparationTool(server, "rental.requirements.prepare_update", "Prepare requirement update", "Load one existing job-product requirement and preview a positive replacement quantity without changing any other requirement.", func(ctx context.Context, input RequirementUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareRequirementUpdate(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "rentalcore", "job_product_requirement_update_draft"), prepared.Warnings, err
	})
	addUpdateTool(server, "rental.requirements.update", "Update rental requirement quantity", "Change only the quantity of one RentalCore job-product requirement. First call rental.requirements.prepare_update, show old and new values, and obtain explicit confirmation.", func(ctx context.Context, input RequirementUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareRequirementUpdate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "rentalcore", "job_product_requirement_update_draft"), prepared.Warnings, err
		}
		if !input.ConfirmUpdate {
			return prepared.response("confirmation_required"), mutationSources(prepared, "rentalcore", "job_product_requirement_update_draft"), []string{"No data was changed."}, nil
		}
		jobID, requirementID := numericID(prepared.Draft["job_id"]), numericID(prepared.Draft["requirement_id"])
		var updated map[string]any
		path := fmt.Sprintf("/api/v1/jobs/%d/requirements/%d", jobID, requirementID)
		if err := api.doJSON(ctx, cfg.RentalURL, path, http.MethodPut, map[string]any{"quantity": prepared.Draft["quantity"]}, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "previous": prepared.Current, "requirement": updated["requirement"]}, []Source{{Service: "rentalcore", Entity: "job_product_requirement", ID: fmt.Sprint(requirementID)}}, nil, nil
	})

	addWritePreparationTool(server, "procurement.orders.prepare_create", "Prepare purchase order", "Resolve the supplier and products, validate every line, dates and currency, calculate the exact total, and identify duplicate supplier order numbers.", func(ctx context.Context, input PurchaseOrderCreateInput) (any, []Source, []string, error) {
		prepared, err := preparePurchaseOrderCreate(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "procurementcore", "purchase_order_draft"), append(untrustedTextWarning(), prepared.Warnings...), err
	})
	addCreateTool(server, "procurement.orders.create", "Create purchase order", "Create one ProcurementCore purchase order. First call procurement.orders.prepare_create, show supplier, every line and the total, and obtain explicit confirmation. ProcurementCore administrator rights are required.", func(ctx context.Context, input PurchaseOrderCreateInput) (any, []Source, []string, error) {
		prepared, err := preparePurchaseOrderCreate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "procurementcore", "purchase_order_draft"), prepared.Warnings, err
		}
		if !input.ConfirmCreation {
			return prepared.response("confirmation_required"), mutationSources(prepared, "procurementcore", "purchase_order_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		var created map[string]any
		payload := cloneMap(prepared.Draft)
		delete(payload, "supplier")
		if err := api.doJSON(ctx, cfg.ProcurementURL, "/api/v1/orders", http.MethodPost, payload, &created); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "created", "purchase_order": created}, []Source{{Service: "procurementcore", Entity: "purchase_order", ID: fmt.Sprint(created["id"])}}, prepared.Warnings, nil
	})

	addWritePreparationTool(server, "warehouse.movements.prepare_create", "Prepare warehouse movement", "Resolve the scanned device or quantity item and validate the action, job, destination zone and quantity before changing physical inventory state.", func(ctx context.Context, input WarehouseMovementCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareWarehouseMovementCreate(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "warehousecore", "movement_draft"), prepared.Warnings, err
	})
	addUpdateTool(server, "warehouse.movements.create", "Book warehouse movement", "Book one confirmed intake, outtake, or transfer through WarehouseCore's audited scan workflow. First call warehouse.movements.prepare_create and obtain explicit confirmation.", func(ctx context.Context, input WarehouseMovementCreateInput) (any, []Source, []string, error) {
		prepared, err := prepareWarehouseMovementCreate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "warehousecore", "movement_draft"), prepared.Warnings, err
		}
		if !input.ConfirmCreation {
			return prepared.response("confirmation_required"), mutationSources(prepared, "warehousecore", "movement_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/scans", http.MethodPost, prepared.Draft, &result); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		if success, ok := result["success"].(bool); ok && !success {
			return nil, nil, prepared.Warnings, fmt.Errorf("WarehouseCore rejected movement: %v", result["message"])
		}
		return map[string]any{"operation_status": "booked", "movement": result}, movementSources(prepared, result), prepared.Warnings, nil
	})

	addWritePreparationTool(server, "warehouse.devices.prepare_update_status", "Prepare device status update", "Load one active device, validate independently managed physical and condition states, and preview the exact change. Job movements must use warehouse.movements.create.", func(ctx context.Context, input DeviceStatusUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareDeviceStatusUpdate(ctx, db, input)
		return prepared.response("draft"), mutationSources(prepared, "warehousecore", "device_status_update_draft"), prepared.Warnings, err
	})
	addUpdateTool(server, "warehouse.devices.update_status", "Update device status", "Change one device's manually managed physical or condition status. First call warehouse.devices.prepare_update_status, show current and new states, and obtain explicit confirmation.", func(ctx context.Context, input DeviceStatusUpdateInput) (any, []Source, []string, error) {
		prepared, err := prepareDeviceStatusUpdate(ctx, db, input)
		if err != nil || !prepared.Ready {
			return prepared.response("needs_input"), mutationSources(prepared, "warehousecore", "device_status_update_draft"), prepared.Warnings, err
		}
		if !input.ConfirmUpdate {
			return prepared.response("confirmation_required"), mutationSources(prepared, "warehousecore", "device_status_update_draft"), append(prepared.Warnings, "No data was changed."), nil
		}
		deviceID := fmt.Sprint(prepared.Draft["device_id"])
		payload := map[string]any{"status": prepared.Draft["status"], "condition_status": prepared.Draft["condition_status"]}
		var result map[string]any
		if err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/devices/"+url.PathEscape(deviceID)+"/status", http.MethodPut, payload, &result); err != nil {
			return nil, nil, prepared.Warnings, err
		}
		return map[string]any{"operation_status": "updated", "previous": prepared.Current, "device": prepared.Draft, "result": result}, []Source{{Service: "warehousecore", Entity: "device", ID: deviceID}}, prepared.Warnings, nil
	})
}

func prepareJobDeviceAssign(ctx context.Context, db *store.Store, input JobDeviceAssignInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{"price": input.Price}}
	if input.Price < 0 {
		p.require("price", "Welcher nicht-negative Mietpreis soll für die Gerätezuweisung gelten?", nil)
	}
	job, options, err := resolveReference(ctx, db, `SELECT j.jobid AS id,COALESCE(NULLIF(TRIM(j.job_code),''),'Job '||j.jobid::text) AS label,concat_ws(' · ',j.description,j.startdate::text,j.enddate::text,s.status) AS context,s.status AS job_status FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid WHERE j.deleted_at IS NULL`, input.JobID, input.JobQuery)
	if err != nil {
		return p, err
	}
	if job == nil {
		p.require("job_id", "Welchem bestehenden Job soll das Gerät zugewiesen werden?", options)
	} else {
		p.Draft["job_id"], p.Draft["job"], p.Draft["job_context"] = job["id"], job["label"], job["context"]
		if isClosedStatus(fmt.Sprint(job["job_status"])) {
			p.require("job_id", "Der gewählte Job ist abgeschlossen oder storniert. Welcher aktive Job soll verwendet werden?", options)
		}
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		p.require("device_id", "Welches aktive serialisierte Gerät soll zugewiesen werden?", nil)
	} else {
		devices, queryErr := db.Query(ctx, `SELECT d.deviceid AS device_id,d.productid AS product_id,p.name AS product,d.serialnumber,d.barcode,d.qr_code,d.status,d.condition_status FROM devices d JOIN products p ON p.productid=d.productid WHERE d.lifecycle_status='active' AND (upper(d.deviceid)=upper($1) OR upper(COALESCE(d.barcode,''))=upper($1) OR upper(COALESCE(d.qr_code,''))=upper($1) OR upper(COALESCE(d.serialnumber,''))=upper($1)) LIMIT 20`, deviceID)
		if queryErr != nil {
			return p, queryErr
		}
		if len(devices) != 1 {
			p.require("device_id", "Die Gerätekennung ist nicht eindeutig oder nicht aktiv. Welche exakte Geräte-ID soll verwendet werden?", devices)
		} else {
			deviceID = fmt.Sprint(devices[0]["device_id"])
			p.Draft["device_id"], p.Draft["device"] = deviceID, devices[0]
		}
	}
	if job != nil && p.Draft["device_id"] != nil {
		rows, queryErr := db.Query(ctx, `SELECT jd.jobid AS job_id,j.job_code,j.startdate AS start_date,j.enddate AS end_date,jd.pack_status FROM job_devices jd JOIN jobs j ON j.jobid=jd.jobid WHERE jd.deviceid=$1 AND jd.jobid=$2`, p.Draft["device_id"], job["id"])
		if queryErr != nil {
			return p, queryErr
		}
		if len(rows) > 0 {
			p.require("existing_assignment", "Das Gerät ist diesem Job bereits zugewiesen; es wird keine zweite Zuweisung angelegt.", rows)
		}
		conflicts, queryErr := db.Query(ctx, `SELECT other.jobid AS job_id,other.job_code,other.startdate AS start_date,other.enddate AS end_date,s.status FROM job_devices jd JOIN jobs other ON other.jobid=jd.jobid JOIN jobs target ON target.jobid=$2 LEFT JOIN status s ON s.statusid=other.statusid WHERE jd.deviceid=$1 AND other.jobid<>target.jobid AND other.deleted_at IS NULL AND other.startdate<=target.enddate AND other.enddate>=target.startdate AND lower(COALESCE(s.status,'')) NOT IN ('abgeschlossen','storniert','completed','cancelled','canceled','closed') LIMIT 20`, p.Draft["device_id"], job["id"])
		if queryErr != nil {
			return p, queryErr
		}
		if len(conflicts) > 0 {
			p.require("availability", "Das Gerät ist in einem überlappenden aktiven Job gebunden. Bitte ein anderes verfügbares Gerät wählen.", conflicts)
		}
	}
	p.finish()
	return p, nil
}

func prepareJobUpdate(ctx context.Context, db *store.Store, input JobUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	job, options, err := resolveReference(ctx, db, `SELECT j.jobid AS id,COALESCE(NULLIF(TRIM(j.job_code),''),'Job '||j.jobid::text) AS label,concat_ws(' · ',j.description,j.startdate::text,j.enddate::text,s.status) AS context FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid WHERE j.deleted_at IS NULL`, input.JobID, input.JobQuery)
	if err != nil {
		return p, err
	}
	if job == nil {
		p.require("job_id", "Welcher bestehende Job soll geändert werden?", options)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT j.jobid AS job_id,j.job_code,j.description,j.customerid AS customer_id,c.companyname AS customer,j.statusid AS status_id,s.status,j.startdate::date AS start_date,j.enddate::date AS end_date,j.venue_id,v.name AS venue,j.revenue,j.discount,j.discount_type,j.job_category_id FROM jobs j LEFT JOIN customers c ON c.customerid=j.customerid LEFT JOIN status s ON s.statusid=j.statusid LEFT JOIN venues v ON v.id=j.venue_id WHERE j.jobid=$1 AND j.deleted_at IS NULL`, job["id"])
	if err != nil || len(rows) != 1 {
		return p, firstError(err, "job not found")
	}
	p.Current = rows[0]
	p.Draft = cloneMap(rows[0])
	for _, field := range []string{"start_date", "end_date"} {
		value := fmt.Sprint(p.Draft[field])
		if len(value) >= len("2006-01-02") {
			value = value[:len("2006-01-02")]
			p.Current[field], p.Draft[field] = value, value
		}
	}
	p.Draft["job_id"], p.Draft["job"] = job["id"], job["label"]
	if value := strings.TrimSpace(input.Description); value != "" {
		p.Draft["description"] = value
	}
	if input.CustomerID > 0 {
		customers, queryErr := db.Query(ctx, `SELECT customerid AS customer_id,COALESCE(NULLIF(companyname,''),concat_ws(' ',firstname,lastname)) AS customer FROM customers WHERE customerid=$1`, input.CustomerID)
		if queryErr != nil {
			return p, queryErr
		}
		if len(customers) != 1 {
			p.require("customer_id", "Der gewählte Kunde existiert nicht. Welche gültige Kunden-ID soll verwendet werden?", customers)
		} else {
			p.Draft["customer_id"], p.Draft["customer"] = input.CustomerID, customers[0]["customer"]
		}
	}
	if input.StatusID > 0 || strings.TrimSpace(input.StatusQuery) != "" {
		status, statusOptions, queryErr := resolveReference(ctx, db, `SELECT statusid AS id,status AS label,'' AS context FROM status`, input.StatusID, input.StatusQuery)
		if queryErr != nil {
			return p, queryErr
		}
		if status == nil {
			p.require("status_id", "Welcher vorhandene Jobstatus soll gesetzt werden?", statusOptions)
		} else {
			p.Draft["status_id"], p.Draft["status"] = status["id"], status["label"]
			statusName := normalizeIdentity(fmt.Sprint(status["label"]))
			if strings.Contains(statusName, "storn") || strings.Contains(statusName, "cancel") {
				p.Warnings = append(p.Warnings, "Der gewählte Status storniert den Job und kann Folgeprozesse für ausgegebene Geräte auslösen.")
			}
		}
	}
	for field, raw := range map[string]string{"start_date": input.StartDate, "end_date": input.EndDate} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, parseErr := time.Parse("2006-01-02", raw)
		if parseErr != nil {
			p.require(field, "Welches gültige Datum im Format YYYY-MM-DD soll verwendet werden?", nil)
		} else {
			p.Draft[field] = parsed.Format("2006-01-02")
		}
	}
	start, startErr := time.Parse("2006-01-02", fmt.Sprint(p.Draft["start_date"]))
	end, endErr := time.Parse("2006-01-02", fmt.Sprint(p.Draft["end_date"]))
	if startErr == nil && endErr == nil && end.Before(start) {
		p.require("date_range", "Das Enddatum liegt vor dem Startdatum. Welcher gültige Zeitraum soll verwendet werden?", nil)
	}
	if input.VenueID != nil {
		if *input.VenueID == 0 {
			p.Draft["venue_id"], p.Draft["venue"] = nil, nil
		} else {
			venues, queryErr := db.Query(ctx, `SELECT id AS venue_id,name FROM venues WHERE id=$1`, *input.VenueID)
			if queryErr != nil {
				return p, queryErr
			}
			if len(venues) != 1 {
				p.require("venue_id", "Der Veranstaltungsort existiert nicht. Welche gültige Venue-ID soll verwendet werden?", venues)
			} else {
				p.Draft["venue_id"], p.Draft["venue"] = *input.VenueID, venues[0]["name"]
			}
		}
	}
	if input.Revenue != nil {
		if *input.Revenue < 0 {
			p.require("revenue", "Welcher nicht-negative Planumsatz soll verwendet werden?", nil)
		} else {
			p.Draft["revenue"] = *input.Revenue
		}
	}
	if mapsEqual(p.Current, p.Draft, "job_id", "job", "job_code") {
		p.require("changes", "Welche Jobdaten sollen tatsächlich geändert werden?", nil)
	}
	p.finish()
	return p, nil
}

func prepareRequirementUpdate(ctx context.Context, db *store.Store, input RequirementUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.RequirementID <= 0 {
		p.require("requirement_id", "Welche bestehende Requirement-ID soll geändert werden?", nil)
	}
	if input.Quantity <= 0 {
		p.require("quantity", "Welche neue positive Bedarfsmenge soll gelten?", nil)
	}
	if input.RequirementID > 0 {
		rows, err := db.Query(ctx, `SELECT r.id AS requirement_id,r.job_id,j.job_code,r.product_id,p.name AS product,r.quantity FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id JOIN products p ON p.productid=r.product_id WHERE r.id=$1 AND j.deleted_at IS NULL`, input.RequirementID)
		if err != nil {
			return p, err
		}
		if len(rows) != 1 {
			p.require("requirement_id", "Die Requirement-ID existiert nicht. Welche gültige ID soll geändert werden?", rows)
		} else {
			p.Current = rows[0]
			p.Draft = cloneMap(rows[0])
			p.Draft["quantity"] = input.Quantity
			if numericID(rows[0]["quantity"]) == int64(input.Quantity) {
				p.require("quantity", "Die neue Menge entspricht der bisherigen Menge. Welche abweichende positive Menge soll gelten?", nil)
			}
		}
	}
	p.finish()
	return p, nil
}

func preparePurchaseOrderCreate(ctx context.Context, db *store.Store, input PurchaseOrderCreateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	supplier, options, err := resolveReference(ctx, db, `SELECT id,name AS label,concat_ws(' · ',code,website) AS context FROM proc_suppliers WHERE active=true`, input.SupplierID, input.SupplierQuery)
	if err != nil {
		return p, err
	}
	if supplier == nil {
		p.require("supplier_id", "Bei welchem aktiven Lieferanten soll bestellt werden?", options)
	} else {
		p.Draft["supplierId"], p.Draft["supplier"] = supplier["id"], supplier["label"]
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "draft"
	}
	if status != "draft" {
		p.require("status", "MCP/KI legt Bestellungen als Entwurf an. Versand und Bestätigung erfolgen getrennt.", "draft")
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "EUR"
	}
	if !containsString([]string{"EUR", "CHF", "USD", "GBP"}, currency) {
		p.require("currency", "Welche unterstützte Währung soll verwendet werden: EUR, CHF, USD oder GBP?", nil)
	}
	p.Draft["status"], p.Draft["currency"] = status, currency
	p.Draft["supplierOrderNumber"], p.Draft["notes"] = strings.TrimSpace(input.SupplierOrderNumber), strings.TrimSpace(input.Notes)
	for field, raw := range map[string]string{"orderDate": input.OrderDate, "expectedDelivery": input.ExpectedDelivery} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, parseErr := time.Parse("2006-01-02", raw)
		if parseErr != nil {
			p.require(toSnake(field), "Welches gültige Datum im Format YYYY-MM-DD soll verwendet werden?", nil)
		} else {
			p.Draft[field] = parsed.UTC().Format(time.RFC3339)
		}
	}
	if len(input.Lines) == 0 {
		p.require("lines", "Welche mindestens eine Bestellposition soll angelegt werden?", nil)
	}
	lines := make([]map[string]any, 0, len(input.Lines))
	var total int64
	for index, line := range input.Lines {
		field := fmt.Sprintf("lines.%d", index)
		description := strings.TrimSpace(line.Description)
		var productID any
		if line.ProductID > 0 {
			products, queryErr := db.Query(ctx, `SELECT id,sku,name FROM proc_products WHERE id=$1 AND active=true`, line.ProductID)
			if queryErr != nil {
				return p, queryErr
			}
			if len(products) != 1 {
				p.require(field+".product_id", "Die Produkt-ID ist nicht aktiv oder existiert nicht. Welche gültige Produkt-ID soll verwendet werden?", products)
			} else {
				productID = line.ProductID
				if description == "" {
					description = fmt.Sprint(products[0]["name"])
				}
			}
		}
		if description == "" {
			p.require(field+".description", "Wie lautet die eindeutige Beschreibung dieser Bestellposition?", nil)
		}
		if line.Quantity <= 0 || math.IsNaN(line.Quantity) || math.IsInf(line.Quantity, 0) || line.Quantity > 1e9 {
			p.require(field+".quantity", "Welche positive Menge soll bestellt werden?", nil)
		}
		if line.UnitPriceCents < 0 || line.UnitPriceCents > 1e9 {
			p.require(field+".unit_price_cents", "Welcher nicht-negative Stückpreis in Cent gilt?", nil)
		}
		unit := strings.TrimSpace(line.Unit)
		if unit == "" {
			unit = "Stk."
		}
		item := map[string]any{"productId": productID, "description": description, "quantity": line.Quantity, "unit": unit, "unitPriceCents": line.UnitPriceCents, "purchaseUrl": strings.TrimSpace(line.PurchaseURL)}
		lines = append(lines, item)
		total += int64(line.Quantity * float64(line.UnitPriceCents))
	}
	p.Draft["lines"], p.Draft["totalCents"] = lines, total
	if supplier != nil && strings.TrimSpace(input.SupplierOrderNumber) != "" {
		duplicates, queryErr := db.Query(ctx, `SELECT id AS purchase_order_id,number,status,supplier_order_number FROM proc_purchase_orders WHERE supplier_id=$1 AND lower(supplier_order_number)=lower($2) LIMIT 20`, supplier["id"], strings.TrimSpace(input.SupplierOrderNumber))
		if queryErr != nil {
			return p, queryErr
		}
		if len(duplicates) > 0 {
			p.require("duplicate_order", "Eine Bestellung mit derselben Lieferanten-Bestellnummer existiert bereits. Bitte die bestehende Bestellung verwenden oder die Nummer korrigieren.", duplicates)
		}
	}
	p.Warnings = append(p.Warnings, "Das Anlegen einer Bestellung erfordert ProcurementCore-Administratorrechte.")
	p.finish()
	return p, nil
}

func prepareWarehouseMovementCreate(ctx context.Context, db *store.Store, input WarehouseMovementCreateInput) (preparedMutation, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	p := preparedMutation{Draft: map[string]any{"scan_code": strings.TrimSpace(input.ScanCode), "action": action}}
	if !containsString([]string{"intake", "outtake", "transfer"}, action) {
		p.require("action", "Welche Lagerbewegung soll gebucht werden: intake, outtake oder transfer?", nil)
	}
	if strings.TrimSpace(input.ScanCode) == "" {
		p.require("scan_code", "Welche exakte Geräte-, Barcode- oder QR-Kennung soll gebucht werden?", nil)
	} else {
		rows, err := db.Query(ctx, `SELECT d.deviceid AS device_id,d.productid AS product_id,p.name AS product,d.status,d.condition_status,d.zone_id,dc.caseid AS case_id FROM devices d JOIN products p ON p.productid=d.productid LEFT JOIN devicescases dc ON dc.deviceid=d.deviceid WHERE d.lifecycle_status='active' AND (upper(d.deviceid)=upper($1) OR upper(COALESCE(d.barcode,''))=upper($1) OR upper(COALESCE(d.qr_code,''))=upper($1) OR upper(COALESCE(d.serialnumber,''))=upper($1) OR EXISTS(SELECT 1 FROM inventory_identifiers ii WHERE ii.entity_type='device' AND ii.entity_key=d.deviceid AND ii.active AND upper(ii.code)=upper($1))) LIMIT 20`, strings.TrimSpace(input.ScanCode))
		if err != nil {
			return p, err
		}
		if len(rows) == 1 {
			p.Current = rows[0]
			p.Draft["scan_code"] = rows[0]["device_id"]
		} else if len(rows) > 1 {
			p.require("scan_code", "Die Kennung ist mehrdeutig. Welche exakte Geräte-ID soll verwendet werden?", rows)
		} else {
			products, queryErr := db.Query(ctx, `SELECT productid AS product_id,name,stock_quantity,generic_barcode FROM products WHERE lifecycle_status='active' AND upper(COALESCE(generic_barcode,''))=upper($1) LIMIT 20`, strings.TrimSpace(input.ScanCode))
			if queryErr != nil {
				return p, queryErr
			}
			if len(products) != 1 {
				p.require("scan_code", "Kein eindeutiges aktives Gerät oder Mengenprodukt wurde gefunden. Welche gültige Kennung soll verwendet werden?", products)
			} else {
				p.Current = products[0]
				if action == "transfer" {
					p.require("action", "Mengenartikel unterstützen keinen einzelnen Zonen-Transfer über den Scanner. Bitte intake oder outtake verwenden.", nil)
				}
			}
		}
	}
	if p.Current["device_id"] != nil && p.Current["case_id"] != nil && (action == "intake" || action == "transfer") {
		p.require("scan_code", "Das Gerät befindet sich in einem Case und muss über den Case-Workflow bewegt werden.", []map[string]any{p.Current})
	}
	if p.Current["device_id"] != nil && (fmt.Sprint(p.Current["status"]) == "on_job" || fmt.Sprint(p.Current["status"]) == "return_pending") && action == "transfer" {
		p.require("action", "Das Gerät muss zuerst eingelagert werden, bevor es zwischen Lagerzonen verschoben werden kann.", nil)
	}
	if action == "outtake" {
		if input.JobID == nil || *input.JobID <= 0 {
			p.require("job_id", "Für welchen aktiven Job soll ausgelagert werden?", nil)
		} else {
			jobs, err := db.Query(ctx, `SELECT j.jobid AS job_id,j.job_code,s.status FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid WHERE j.jobid=$1 AND j.deleted_at IS NULL`, *input.JobID)
			if err != nil {
				return p, err
			}
			if len(jobs) != 1 || isClosedStatus(fmt.Sprint(firstValue(jobs, "status"))) {
				p.require("job_id", "Der Job existiert nicht oder ist abgeschlossen/storniert. Welche gültige Job-ID soll verwendet werden?", jobs)
			} else {
				p.Draft["job_id"] = *input.JobID
			}
		}
	}
	if action == "intake" || action == "transfer" {
		if input.ZoneID == nil || *input.ZoneID <= 0 {
			p.require("zone_id", "In welche aktive Lagerzone soll eingelagert oder verschoben werden?", nil)
		} else {
			zones, err := db.Query(ctx, `SELECT zone_id,code,name,is_active FROM storage_zones WHERE zone_id=$1 AND is_active`, *input.ZoneID)
			if err != nil {
				return p, err
			}
			if len(zones) != 1 {
				p.require("zone_id", "Die Zielzone existiert nicht oder ist archiviert. Welche aktive Zone soll verwendet werden?", zones)
			} else {
				p.Draft["zone_id"] = *input.ZoneID
			}
		}
	}
	if input.Quantity != nil {
		if *input.Quantity <= 0 {
			p.require("quantity", "Welche positive Menge soll bewegt werden?", nil)
		} else {
			p.Draft["quantity"] = *input.Quantity
		}
	}
	if notes := strings.TrimSpace(input.Notes); notes != "" {
		p.Draft["notes"] = notes
	}
	p.Warnings = append(p.Warnings, "Diese Aktion ändert den physischen Lagerzustand und wird im WarehouseCore-Bewegungsprotokoll erfasst.")
	p.finish()
	return p, nil
}

func prepareDeviceStatusUpdate(ctx context.Context, db *store.Store, input DeviceStatusUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{"device_id": strings.TrimSpace(input.DeviceID)}}
	if strings.TrimSpace(input.DeviceID) == "" {
		p.require("device_id", "Welches aktive Gerät soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	rows, err := db.Query(ctx, `SELECT d.deviceid AS device_id,d.productid AS product_id,p.name AS product,d.status,d.condition_status,d.zone_id,dc.caseid AS case_id FROM devices d JOIN products p ON p.productid=d.productid LEFT JOIN devicescases dc ON dc.deviceid=d.deviceid WHERE d.deviceid=$1 AND d.lifecycle_status='active'`, strings.TrimSpace(input.DeviceID))
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("device_id", "Die Geräte-ID existiert nicht oder ist nicht aktiv. Welche gültige Geräte-ID soll verwendet werden?", rows)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	p.Draft = cloneMap(rows[0])
	status, condition := strings.ToLower(strings.TrimSpace(input.Status)), strings.ToLower(strings.TrimSpace(input.ConditionStatus))
	if status != "" {
		if !containsString([]string{"in_storage", "location_unknown"}, status) {
			p.require("status", "Welcher manuell erlaubte Lagerstatus soll verwendet werden: in_storage oder location_unknown?", nil)
		} else if fmt.Sprint(rows[0]["status"]) == "on_job" || fmt.Sprint(rows[0]["status"]) == "return_pending" {
			p.require("status", "Aktive Ausgabe- oder Rücklaufzustände müssen zuerst über warehouse.movements.create abgeschlossen werden.", nil)
		} else if status == "in_storage" && rows[0]["zone_id"] == nil && rows[0]["case_id"] == nil {
			p.require("status", "in_storage erfordert bereits einen Lagerplatz oder ein Case; bitte zuerst eine Lagerbewegung buchen.", nil)
		} else {
			p.Draft["status"] = status
		}
	}
	if condition != "" {
		if !containsString([]string{"available", "blocked", "defective", "maintenance", "retired"}, condition) {
			p.require("condition_status", "Welcher gültige Betriebszustand soll verwendet werden: available, blocked, defective, maintenance oder retired?", nil)
		} else {
			p.Draft["condition_status"] = condition
		}
	}
	if status == "" && condition == "" {
		p.require("changes", "Soll der Lagerstatus oder der Betriebszustand geändert werden?", nil)
	} else if fmt.Sprint(p.Draft["status"]) == fmt.Sprint(rows[0]["status"]) && fmt.Sprint(p.Draft["condition_status"]) == fmt.Sprint(rows[0]["condition_status"]) {
		p.require("changes", "Die neuen Zustände entsprechen den aktuellen. Welche tatsächliche Änderung soll erfolgen?", nil)
	}
	p.Warnings = append(p.Warnings, "Statusänderungen werden in der WarehouseCore-Gerätehistorie protokolliert.")
	p.finish()
	return p, nil
}

func mutationSources(prepared preparedMutation, service, fallbackEntity string) []Source {
	for _, key := range []struct{ field, entity string }{{"requirement_id", "job_product_requirement"}, {"job_id", "job"}, {"device_id", "device"}, {"supplierId", "supplier"}} {
		if value, ok := prepared.Draft[key.field]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return []Source{{Service: service, Entity: key.entity, ID: fmt.Sprint(value)}}
		}
	}
	return []Source{{Service: service, Entity: fallbackEntity}}
}

func movementSources(prepared preparedMutation, result map[string]any) []Source {
	sources := mutationSources(prepared, "warehousecore", "movement")
	if movement, ok := result["movement"].(map[string]any); ok && movement["movement_id"] != nil {
		sources = append(sources, Source{Service: "warehousecore", Entity: "device_movement", ID: fmt.Sprint(movement["movement_id"])})
	}
	return sources
}

func cloneWithout(input map[string]any, keys ...string) map[string]any {
	result := cloneMap(input)
	for _, key := range keys {
		delete(result, key)
	}
	return result
}

func mapsEqual(a, b map[string]any, ignored ...string) bool {
	ignore := map[string]bool{}
	for _, key := range ignored {
		ignore[key] = true
	}
	for key, value := range a {
		if !ignore[key] && fmt.Sprint(value) != fmt.Sprint(b[key]) {
			return false
		}
	}
	for key, value := range b {
		if !ignore[key] && fmt.Sprint(value) != fmt.Sprint(a[key]) {
			return false
		}
	}
	return true
}

func firstError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errorsNew(fallback)
}

func firstValue(rows []map[string]any, key string) any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0][key]
}

func isClosedStatus(value string) bool {
	value = normalizeIdentity(value)
	return strings.Contains(value, "closed") || strings.Contains(value, "completed") || strings.Contains(value, "cancel") || strings.Contains(value, "storn") || strings.Contains(value, "abgeschlossen")
}

func toSnake(value string) string {
	var result []rune
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			result = append(result, '_', r+('a'-'A'))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

type QueryRecordsInput struct {
	Queries []EntityQueryInput `json:"queries" jsonschema:"One to eight named, independent queries over curated Cores entities."`
	Joins   []QueryJoinInput   `json:"joins,omitempty" jsonschema:"Optional joins between query aliases. Use a catalog relationship or explicit fields."`
}

type EntityQueryInput struct {
	Alias   string             `json:"alias" jsonschema:"Unique result name using lowercase letters, numbers and underscores."`
	Entity  string             `json:"entity" jsonschema:"Curated entity name from cores.query.catalog, for example rental.jobs or warehouse.products."`
	Search  string             `json:"search,omitempty" jsonschema:"Case-insensitive text search across the entity's documented searchable fields."`
	Fields  []string           `json:"fields,omitempty" jsonschema:"Fields to return. Empty selects the entity's documented default fields."`
	Filters []QueryFilterInput `json:"filters,omitempty" jsonschema:"AND-combined, typed filters over documented fields."`
	Sort    []QuerySortInput   `json:"sort,omitempty" jsonschema:"Validated sort fields and directions."`
	Limit   int                `json:"limit,omitempty" jsonschema:"Maximum records for this query; defaults to 50 and is capped server-side."`
	Offset  int                `json:"offset,omitempty" jsonschema:"Number of matching records to skip."`
}

type QueryFilterInput struct {
	Field    string   `json:"field" jsonschema:"Documented field name."`
	Operator string   `json:"operator" jsonschema:"One of eq, ne, contains, not_contains, prefix, gt, gte, lt, lte, in, not_in, between, is_null, is_not_null."`
	Value    string   `json:"value,omitempty" jsonschema:"Typed value encoded as text. Dates use YYYY-MM-DD or RFC3339."`
	Values   []string `json:"values,omitempty" jsonschema:"Values for in, not_in, or the two bounds for between."`
}

type QuerySortInput struct {
	Field     string `json:"field" jsonschema:"Documented field name or aggregate alias."`
	Direction string `json:"direction,omitempty" jsonschema:"asc or desc; defaults to asc."`
	Nulls     string `json:"nulls,omitempty" jsonschema:"first or last; leave empty for the database default."`
}

type QueryJoinInput struct {
	Alias        string `json:"alias" jsonschema:"Unique name for the joined result."`
	Left         string `json:"left" jsonschema:"Left query alias."`
	Right        string `json:"right" jsonschema:"Right query alias."`
	Relationship string `json:"relationship,omitempty" jsonschema:"Relationship name from cores.query.catalog. When set, field names are inferred."`
	LeftField    string `json:"left_field,omitempty" jsonschema:"Explicit left field when no catalog relationship is used."`
	RightField   string `json:"right_field,omitempty" jsonschema:"Explicit right field when no catalog relationship is used."`
	Type         string `json:"type,omitempty" jsonschema:"inner or left; defaults to inner."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum joined rows; capped server-side."`
}

type QueryAggregateInput struct {
	Entity  string                 `json:"entity" jsonschema:"Curated entity name from cores.query.catalog."`
	Search  string                 `json:"search,omitempty" jsonschema:"Case-insensitive search over documented searchable fields."`
	Filters []QueryFilterInput     `json:"filters,omitempty" jsonschema:"AND-combined typed filters."`
	GroupBy []string               `json:"group_by,omitempty" jsonschema:"Up to four documented fields used as grouping dimensions."`
	Metrics []QueryAggregateMetric `json:"metrics,omitempty" jsonschema:"Up to eight aggregate metrics. Empty returns count as record_count."`
	Sort    []QuerySortInput       `json:"sort,omitempty" jsonschema:"Sort by a group field or metric alias."`
	Limit   int                    `json:"limit,omitempty" jsonschema:"Maximum groups; capped server-side."`
}

type QueryAggregateMetric struct {
	Function string `json:"function" jsonschema:"count, count_distinct, sum, avg, min, or max."`
	Field    string `json:"field,omitempty" jsonschema:"Documented field. count may omit it to count rows."`
	Alias    string `json:"alias,omitempty" jsonschema:"Optional lowercase result name; otherwise generated from function and field."`
}

type queryFieldKind string

const (
	queryString  queryFieldKind = "string"
	queryInteger queryFieldKind = "integer"
	queryNumber  queryFieldKind = "number"
	queryBoolean queryFieldKind = "boolean"
	queryTime    queryFieldKind = "datetime"
)

type queryEntitySpec struct {
	Service       string
	SourceEntity  string
	BaseSQL       string
	Fields        map[string]queryFieldKind
	DefaultFields []string
	SearchFields  []string
}

type queryRelationship struct {
	Name        string
	LeftEntity  string
	LeftField   string
	RightEntity string
	RightField  string
	Description string
}

type querySection struct {
	Entity string           `json:"entity"`
	Count  int              `json:"count"`
	Rows   []map[string]any `json:"rows"`
}

type joinedSection struct {
	Left         string           `json:"left"`
	Right        string           `json:"right"`
	Relationship string           `json:"relationship,omitempty"`
	Count        int              `json:"count"`
	Rows         []map[string]any `json:"rows"`
}

var queryAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

var queryEntities = map[string]queryEntitySpec{
	"rental.jobs": {
		Service: "rentalcore", SourceEntity: "job",
		BaseSQL: `SELECT j.jobid AS job_id,j.job_code,j.description,s.status,j.startdate AS start_date,j.enddate AS end_date,
                   j.customerid AS customer_id,COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS customer,
                   j.venue_id,v.name AS venue,j.revenue,j.final_revenue,j.updated_at
              FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid LEFT JOIN customers c ON c.customerid=j.customerid
              LEFT JOIN venues v ON v.id=j.venue_id WHERE j.deleted_at IS NULL`,
		Fields:        map[string]queryFieldKind{"job_id": queryInteger, "job_code": queryString, "description": queryString, "status": queryString, "start_date": queryTime, "end_date": queryTime, "customer_id": queryInteger, "customer": queryString, "venue_id": queryInteger, "venue": queryString, "revenue": queryNumber, "final_revenue": queryNumber, "updated_at": queryTime},
		DefaultFields: []string{"job_id", "job_code", "description", "status", "start_date", "end_date", "customer", "venue", "revenue", "final_revenue", "updated_at"},
		SearchFields:  []string{"job_code", "description", "status", "customer", "venue"},
	},
	"rental.requirements": {
		Service: "rentalcore", SourceEntity: "job_product_requirement",
		BaseSQL: `SELECT r.id AS requirement_id,r.job_id,j.job_code,j.startdate AS start_date,j.enddate AS end_date,
                   r.product_id,p.product_code,p.name AS product,p.tracking_mode,r.quantity
              FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id JOIN products p ON p.productid=r.product_id
             WHERE j.deleted_at IS NULL`,
		Fields:        map[string]queryFieldKind{"requirement_id": queryInteger, "job_id": queryInteger, "job_code": queryString, "start_date": queryTime, "end_date": queryTime, "product_id": queryInteger, "product_code": queryString, "product": queryString, "tracking_mode": queryString, "quantity": queryNumber},
		DefaultFields: []string{"requirement_id", "job_id", "job_code", "start_date", "end_date", "product_id", "product_code", "product", "tracking_mode", "quantity"},
		SearchFields:  []string{"job_code", "product_code", "product", "tracking_mode"},
	},
	"rental.customers": {
		Service: "rentalcore", SourceEntity: "customer",
		BaseSQL: `SELECT c.customerid AS customer_id,COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS name,
                   c.customertype AS customer_type,c.city,c.country,c.is_customer,c.is_supplier,c.is_archived
              FROM customers c`,
		Fields:        map[string]queryFieldKind{"customer_id": queryInteger, "name": queryString, "customer_type": queryString, "city": queryString, "country": queryString, "is_customer": queryBoolean, "is_supplier": queryBoolean, "is_archived": queryBoolean},
		DefaultFields: []string{"customer_id", "name", "customer_type", "city", "country", "is_customer", "is_supplier", "is_archived"},
		SearchFields:  []string{"name", "customer_type", "city", "country"},
	},
	"rental.venues": {
		Service: "rentalcore", SourceEntity: "venue",
		BaseSQL:       `SELECT v.id AS venue_id,v.name,v.city,v.zip,v.updated_at FROM venues v`,
		Fields:        map[string]queryFieldKind{"venue_id": queryInteger, "name": queryString, "city": queryString, "zip": queryString, "updated_at": queryTime},
		DefaultFields: []string{"venue_id", "name", "city", "zip", "updated_at"}, SearchFields: []string{"name", "city", "zip"},
	},
	"warehouse.products": {
		Service: "warehousecore", SourceEntity: "product",
		BaseSQL: `SELECT p.productid AS product_id,p.product_code,p.name,c.name AS category,m.name AS manufacturer,p.model_number,
                   p.product_type,p.product_kind,p.tracking_mode,p.lifecycle_status,p.is_accessory,p.is_consumable,p.stock_quantity,p.min_stock_level,p.price_per_unit,
                   (SELECT count(*) FROM devices d WHERE d.productid=p.productid) AS total_devices,
                   (SELECT count(*) FROM devices d WHERE d.productid=p.productid AND d.condition_status='available') AS available_devices,p.updated_at
              FROM products p LEFT JOIN categories c ON c.categoryid=p.categoryid LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid
             WHERE COALESCE(p.lifecycle_status,'active')<>'deleted'`,
		Fields:        map[string]queryFieldKind{"product_id": queryInteger, "product_code": queryString, "name": queryString, "category": queryString, "manufacturer": queryString, "model_number": queryString, "product_type": queryString, "product_kind": queryString, "tracking_mode": queryString, "lifecycle_status": queryString, "is_accessory": queryBoolean, "is_consumable": queryBoolean, "stock_quantity": queryNumber, "min_stock_level": queryNumber, "price_per_unit": queryNumber, "total_devices": queryInteger, "available_devices": queryInteger, "updated_at": queryTime},
		DefaultFields: []string{"product_id", "product_code", "name", "category", "manufacturer", "model_number", "tracking_mode", "lifecycle_status", "stock_quantity", "min_stock_level", "total_devices", "available_devices", "updated_at"},
		SearchFields:  []string{"product_code", "name", "category", "manufacturer", "model_number", "product_type", "product_kind", "tracking_mode", "lifecycle_status"},
	},
	"warehouse.devices": {
		Service: "warehousecore", SourceEntity: "device",
		BaseSQL: `SELECT d.deviceid AS device_id,d.productid AS product_id,p.product_code,p.name AS product,d.serialnumber AS serial_number,d.barcode,d.qr_code,
                   d.status,d.condition_status,d.zone_id,z.code AS zone_code,z.name AS zone,d.current_case_id AS case_id,c.name AS current_case,
                   d.purchasedate AS purchase_date,d.lastmaintenance AS last_maintenance,d.nextmaintenance AS next_maintenance,d.condition_rating,d.usage_hours,d.total_revenue,d.updated_at
              FROM devices d JOIN products p ON p.productid=d.productid LEFT JOIN storage_zones z ON z.zone_id=d.zone_id LEFT JOIN cases c ON c.caseid=d.current_case_id`,
		Fields:        map[string]queryFieldKind{"device_id": queryString, "product_id": queryInteger, "product_code": queryString, "product": queryString, "serial_number": queryString, "barcode": queryString, "qr_code": queryString, "status": queryString, "condition_status": queryString, "zone_id": queryInteger, "zone_code": queryString, "zone": queryString, "case_id": queryInteger, "current_case": queryString, "purchase_date": queryTime, "last_maintenance": queryTime, "next_maintenance": queryTime, "condition_rating": queryNumber, "usage_hours": queryNumber, "total_revenue": queryNumber, "updated_at": queryTime},
		DefaultFields: []string{"device_id", "product_id", "product_code", "product", "serial_number", "barcode", "status", "condition_status", "zone_code", "zone", "case_id", "current_case", "next_maintenance", "condition_rating", "usage_hours", "updated_at"},
		SearchFields:  []string{"device_id", "product_code", "product", "serial_number", "barcode", "qr_code", "status", "condition_status", "zone_code", "zone", "current_case"},
	},
	"warehouse.defects": {
		Service: "warehousecore", SourceEntity: "defect",
		BaseSQL: `SELECT dr.defect_id,dr.device_id,d.productid AS product_id,p.name AS product,dr.severity,dr.status,dr.description,dr.created_at,dr.updated_at
              FROM defect_reports dr JOIN devices d ON d.deviceid=dr.device_id JOIN products p ON p.productid=d.productid`,
		Fields:        map[string]queryFieldKind{"defect_id": queryInteger, "device_id": queryString, "product_id": queryInteger, "product": queryString, "severity": queryString, "status": queryString, "description": queryString, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"defect_id", "device_id", "product_id", "product", "severity", "status", "description", "created_at", "updated_at"}, SearchFields: []string{"device_id", "product", "severity", "status", "description"},
	},
	"warehouse.maintenance": {
		Service: "warehousecore", SourceEntity: "maintenance_order",
		BaseSQL: `SELECT mo.order_id AS maintenance_id,mo.device_id,d.productid AS product_id,p.name AS product,mo.title,mo.order_type,mo.priority,mo.status,
                   mo.due_at,mo.completed_at,mo.cost,mo.created_at,mo.updated_at
              FROM maintenance_orders mo JOIN devices d ON d.deviceid=mo.device_id JOIN products p ON p.productid=d.productid`,
		Fields:        map[string]queryFieldKind{"maintenance_id": queryInteger, "device_id": queryString, "product_id": queryInteger, "product": queryString, "title": queryString, "order_type": queryString, "priority": queryString, "status": queryString, "due_at": queryTime, "completed_at": queryTime, "cost": queryNumber, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"maintenance_id", "device_id", "product_id", "product", "title", "order_type", "priority", "status", "due_at", "completed_at", "cost", "updated_at"}, SearchFields: []string{"device_id", "product", "title", "order_type", "priority", "status"},
	},
	"warehouse.cases": {
		Service: "warehousecore", SourceEntity: "case",
		BaseSQL: `SELECT c.caseid AS case_id,c.name,c.barcode,c.rfid_tag,c.case_type,c.status,c.workflow_status,c.zone_id,z.code AS zone_code,z.name AS zone,
                   c.current_job_id,c.sealed_at,c.weight,c.max_weight_kg,c.updated_at
              FROM cases c LEFT JOIN storage_zones z ON z.zone_id=c.zone_id`,
		Fields:        map[string]queryFieldKind{"case_id": queryInteger, "name": queryString, "barcode": queryString, "rfid_tag": queryString, "case_type": queryString, "status": queryString, "workflow_status": queryString, "zone_id": queryInteger, "zone_code": queryString, "zone": queryString, "current_job_id": queryInteger, "sealed_at": queryTime, "weight": queryNumber, "max_weight_kg": queryNumber, "updated_at": queryTime},
		DefaultFields: []string{"case_id", "name", "barcode", "case_type", "status", "workflow_status", "zone_code", "zone", "current_job_id", "sealed_at", "weight", "updated_at"}, SearchFields: []string{"name", "barcode", "rfid_tag", "case_type", "status", "workflow_status", "zone_code", "zone"},
	},
	"warehouse.tasks": {
		Service: "warehousecore", SourceEntity: "warehouse_task",
		BaseSQL: `SELECT wt.task_id,wt.task_type,wt.status,wt.priority,wt.from_zone_id,fz.name AS from_zone,wt.to_zone_id,tz.name AS to_zone,
                   wt.case_id,c.name AS case_name,wt.device_id,wt.product_id,p.name AS product,wt.quantity,wt.job_id,wt.assigned_to,wt.due_at,wt.notes,wt.created_at
              FROM warehouse_tasks wt LEFT JOIN storage_zones fz ON fz.zone_id=wt.from_zone_id LEFT JOIN storage_zones tz ON tz.zone_id=wt.to_zone_id
              LEFT JOIN cases c ON c.caseid=wt.case_id LEFT JOIN products p ON p.productid=wt.product_id`,
		Fields:        map[string]queryFieldKind{"task_id": queryInteger, "task_type": queryString, "status": queryString, "priority": queryString, "from_zone_id": queryInteger, "from_zone": queryString, "to_zone_id": queryInteger, "to_zone": queryString, "case_id": queryInteger, "case_name": queryString, "device_id": queryString, "product_id": queryInteger, "product": queryString, "quantity": queryNumber, "job_id": queryInteger, "assigned_to": queryString, "due_at": queryTime, "notes": queryString, "created_at": queryTime},
		DefaultFields: []string{"task_id", "task_type", "status", "priority", "from_zone", "to_zone", "case_id", "case_name", "device_id", "product_id", "product", "quantity", "job_id", "assigned_to", "due_at", "created_at"}, SearchFields: []string{"task_type", "status", "priority", "from_zone", "to_zone", "case_name", "device_id", "product", "assigned_to", "notes"},
	},
	"planner.plans": {
		Service: "plannercore", SourceEntity: "plan",
		BaseSQL:       `SELECT p.id AS plan_id,p.name,p.description,p.is_favorite,p.is_template,p.created_by,p.created_at,p.updated_at,p.archived_at FROM planner_plans p`,
		Fields:        map[string]queryFieldKind{"plan_id": queryString, "name": queryString, "description": queryString, "is_favorite": queryBoolean, "is_template": queryBoolean, "created_by": queryString, "created_at": queryTime, "updated_at": queryTime, "archived_at": queryTime},
		DefaultFields: []string{"plan_id", "name", "description", "is_favorite", "is_template", "created_by", "created_at", "updated_at", "archived_at"}, SearchFields: []string{"name", "description", "created_by"},
	},
	"planner.tasks": {
		Service: "plannercore", SourceEntity: "task",
		BaseSQL: `SELECT t.id AS task_id,t.title,t.plan_id,p.name AS plan,t.bucket_id,b.name AS bucket,t.priority,t.progress,t.start_date,t.due_date,t.completed_at,
                   t.checklist_completed_count,t.checklist_total_count,t.recurrence,COALESCE(string_agg(DISTINCT a.user_id,', '),'') AS assignees,
                   COALESCE(string_agg(DISTINCT l.name,', '),'') AS labels,t.created_at,t.updated_at
              FROM planner_tasks t JOIN planner_plans p ON p.id=t.plan_id LEFT JOIN planner_buckets b ON b.id=t.bucket_id
              LEFT JOIN planner_task_assignees a ON a.task_id=t.id LEFT JOIN planner_task_labels tl ON tl.task_id=t.id LEFT JOIN planner_labels l ON l.id=tl.label_id
             GROUP BY t.id,p.name,b.name`,
		Fields:        map[string]queryFieldKind{"task_id": queryString, "title": queryString, "plan_id": queryString, "plan": queryString, "bucket_id": queryString, "bucket": queryString, "priority": queryString, "progress": queryNumber, "start_date": queryTime, "due_date": queryTime, "completed_at": queryTime, "checklist_completed_count": queryInteger, "checklist_total_count": queryInteger, "recurrence": queryString, "assignees": queryString, "labels": queryString, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"task_id", "title", "plan_id", "plan", "bucket", "priority", "progress", "start_date", "due_date", "completed_at", "assignees", "labels", "updated_at"}, SearchFields: []string{"title", "plan", "bucket", "priority", "recurrence", "assignees", "labels"},
	},
	"procurement.products": {
		Service: "procurementcore", SourceEntity: "product",
		BaseSQL: `SELECT p.id AS procurement_product_id,p.sku,p.name,p.description,c.name AS category,p.unit,p.manufacturer,p.model,p.reorder_point,p.target_stock,
                   p.active,l.warehouse_product_id,p.created_at,p.updated_at
              FROM proc_products p LEFT JOIN proc_categories c ON c.id=p.category_id LEFT JOIN core_product_links l ON l.procurement_product_id=p.id`,
		Fields:        map[string]queryFieldKind{"procurement_product_id": queryInteger, "sku": queryString, "name": queryString, "description": queryString, "category": queryString, "unit": queryString, "manufacturer": queryString, "model": queryString, "reorder_point": queryNumber, "target_stock": queryNumber, "active": queryBoolean, "warehouse_product_id": queryInteger, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"procurement_product_id", "sku", "name", "category", "unit", "manufacturer", "model", "reorder_point", "target_stock", "active", "warehouse_product_id", "updated_at"}, SearchFields: []string{"sku", "name", "description", "category", "unit", "manufacturer", "model"},
	},
	"procurement.offers": {
		Service: "procurementcore", SourceEntity: "offer",
		BaseSQL: `SELECT o.id AS offer_id,o.product_id AS procurement_product_id,p.sku,p.name AS product,o.supplier_id,s.name AS supplier,s.preferred,s.rating,s.risk_level,
                   o.supplier_sku,o.price_cents,o.currency,o.minimum_quantity,o.pack_size,round(o.price_cents/NULLIF(o.pack_size,0),2) AS price_per_unit_cents,
                   o.lead_days,o.valid_until,o.last_checked_at,o.active
              FROM proc_offers o JOIN proc_products p ON p.id=o.product_id JOIN proc_suppliers s ON s.id=o.supplier_id`,
		Fields:        map[string]queryFieldKind{"offer_id": queryInteger, "procurement_product_id": queryInteger, "sku": queryString, "product": queryString, "supplier_id": queryInteger, "supplier": queryString, "preferred": queryBoolean, "rating": queryNumber, "risk_level": queryString, "supplier_sku": queryString, "price_cents": queryNumber, "currency": queryString, "minimum_quantity": queryNumber, "pack_size": queryNumber, "price_per_unit_cents": queryNumber, "lead_days": queryNumber, "valid_until": queryTime, "last_checked_at": queryTime, "active": queryBoolean},
		DefaultFields: []string{"offer_id", "procurement_product_id", "sku", "product", "supplier_id", "supplier", "preferred", "rating", "risk_level", "price_cents", "currency", "pack_size", "price_per_unit_cents", "lead_days", "valid_until", "last_checked_at", "active"}, SearchFields: []string{"sku", "product", "supplier", "risk_level", "supplier_sku", "currency"},
	},
	"procurement.suppliers": {
		Service: "procurementcore", SourceEntity: "supplier",
		BaseSQL:       `SELECT s.id AS supplier_id,s.name,s.code,s.website,s.payment_terms,s.default_lead_days,s.rating,s.preferred,s.risk_level,s.active,s.created_at,s.updated_at FROM proc_suppliers s`,
		Fields:        map[string]queryFieldKind{"supplier_id": queryInteger, "name": queryString, "code": queryString, "website": queryString, "payment_terms": queryString, "default_lead_days": queryNumber, "rating": queryNumber, "preferred": queryBoolean, "risk_level": queryString, "active": queryBoolean, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"supplier_id", "name", "code", "website", "payment_terms", "default_lead_days", "rating", "preferred", "risk_level", "active", "updated_at"}, SearchFields: []string{"name", "code", "payment_terms", "risk_level"},
	},
	"procurement.requisitions": {
		Service: "procurementcore", SourceEntity: "requisition",
		BaseSQL: `SELECT r.id AS requisition_id,r.number,r.title,r.status,r.requester_name,r.cost_center,r.justification,r.needed_by,r.estimated_total_cents,
                   r.approved_by_name,r.decision_note,r.submitted_at,r.decided_at,r.created_at,r.updated_at FROM proc_requisitions r`,
		Fields:        map[string]queryFieldKind{"requisition_id": queryInteger, "number": queryString, "title": queryString, "status": queryString, "requester_name": queryString, "cost_center": queryString, "justification": queryString, "needed_by": queryTime, "estimated_total_cents": queryNumber, "approved_by_name": queryString, "decision_note": queryString, "submitted_at": queryTime, "decided_at": queryTime, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"requisition_id", "number", "title", "status", "requester_name", "cost_center", "needed_by", "estimated_total_cents", "approved_by_name", "submitted_at", "decided_at", "updated_at"}, SearchFields: []string{"number", "title", "status", "requester_name", "cost_center", "justification", "approved_by_name", "decision_note"},
	},
	"procurement.requisition_lines": {
		Service: "procurementcore", SourceEntity: "requisition_line",
		BaseSQL: `SELECT rl.id AS requisition_line_id,rl.requisition_id,r.number AS requisition_number,r.status,rl.product_id AS procurement_product_id,
                   COALESCE(p.name,rl.description) AS product,rl.quantity,rl.unit,rl.estimated_price_cents,r.needed_by
              FROM proc_requisition_lines rl JOIN proc_requisitions r ON r.id=rl.requisition_id LEFT JOIN proc_products p ON p.id=rl.product_id`,
		Fields:        map[string]queryFieldKind{"requisition_line_id": queryInteger, "requisition_id": queryInteger, "requisition_number": queryString, "status": queryString, "procurement_product_id": queryInteger, "product": queryString, "quantity": queryNumber, "unit": queryString, "estimated_price_cents": queryNumber, "needed_by": queryTime},
		DefaultFields: []string{"requisition_line_id", "requisition_id", "requisition_number", "status", "procurement_product_id", "product", "quantity", "unit", "estimated_price_cents", "needed_by"}, SearchFields: []string{"requisition_number", "status", "product", "unit"},
	},
	"procurement.orders": {
		Service: "procurementcore", SourceEntity: "purchase_order",
		BaseSQL: `SELECT po.id AS purchase_order_id,po.number,po.status,po.supplier_id,s.name AS supplier,po.currency,po.total_cents,po.ordered_by_name,
                   po.order_date,po.expected_delivery,po.created_at,po.updated_at FROM proc_purchase_orders po LEFT JOIN proc_suppliers s ON s.id=po.supplier_id`,
		Fields:        map[string]queryFieldKind{"purchase_order_id": queryInteger, "number": queryString, "status": queryString, "supplier_id": queryInteger, "supplier": queryString, "currency": queryString, "total_cents": queryNumber, "ordered_by_name": queryString, "order_date": queryTime, "expected_delivery": queryTime, "created_at": queryTime, "updated_at": queryTime},
		DefaultFields: []string{"purchase_order_id", "number", "status", "supplier_id", "supplier", "currency", "total_cents", "ordered_by_name", "order_date", "expected_delivery", "updated_at"}, SearchFields: []string{"number", "status", "supplier", "currency", "ordered_by_name"},
	},
	"procurement.order_lines": {
		Service: "procurementcore", SourceEntity: "purchase_order_line",
		BaseSQL: `SELECT pol.id AS order_line_id,pol.purchase_order_id,po.number AS order_number,po.status,po.supplier_id,s.name AS supplier,
                   pol.product_id AS procurement_product_id,COALESCE(p.name,pol.description) AS product,pol.quantity,pol.received_quantity,
                   pol.unit,pol.unit_price_cents,po.currency,po.expected_delivery
              FROM proc_purchase_order_lines pol JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id
              LEFT JOIN proc_products p ON p.id=pol.product_id LEFT JOIN proc_suppliers s ON s.id=po.supplier_id`,
		Fields:        map[string]queryFieldKind{"order_line_id": queryInteger, "purchase_order_id": queryInteger, "order_number": queryString, "status": queryString, "supplier_id": queryInteger, "supplier": queryString, "procurement_product_id": queryInteger, "product": queryString, "quantity": queryNumber, "received_quantity": queryNumber, "unit": queryString, "unit_price_cents": queryNumber, "currency": queryString, "expected_delivery": queryTime},
		DefaultFields: []string{"order_line_id", "purchase_order_id", "order_number", "status", "supplier_id", "supplier", "procurement_product_id", "product", "quantity", "received_quantity", "unit", "unit_price_cents", "currency", "expected_delivery"}, SearchFields: []string{"order_number", "status", "supplier", "product", "unit", "currency"},
	},
}

var queryRelationships = []queryRelationship{
	{Name: "rental.job_customer", LeftEntity: "rental.jobs", LeftField: "customer_id", RightEntity: "rental.customers", RightField: "customer_id", Description: "Customer responsible for a rental job."},
	{Name: "rental.job_venue", LeftEntity: "rental.jobs", LeftField: "venue_id", RightEntity: "rental.venues", RightField: "venue_id", Description: "Venue assigned to a rental job."},
	{Name: "rental.job_requirements", LeftEntity: "rental.jobs", LeftField: "job_id", RightEntity: "rental.requirements", RightField: "job_id", Description: "Material requirements belonging to a rental job."},
	{Name: "inventory.requirement_product", LeftEntity: "rental.requirements", LeftField: "product_id", RightEntity: "warehouse.products", RightField: "product_id", Description: "Warehouse product requested by a rental job."},
	{Name: "warehouse.product_devices", LeftEntity: "warehouse.products", LeftField: "product_id", RightEntity: "warehouse.devices", RightField: "product_id", Description: "Serialized devices belonging to a product."},
	{Name: "warehouse.product_defects", LeftEntity: "warehouse.products", LeftField: "product_id", RightEntity: "warehouse.defects", RightField: "product_id", Description: "Defects affecting a warehouse product."},
	{Name: "warehouse.device_defects", LeftEntity: "warehouse.devices", LeftField: "device_id", RightEntity: "warehouse.defects", RightField: "device_id", Description: "Defects reported for a serialized device."},
	{Name: "warehouse.device_maintenance", LeftEntity: "warehouse.devices", LeftField: "device_id", RightEntity: "warehouse.maintenance", RightField: "device_id", Description: "Maintenance orders for a serialized device."},
	{Name: "warehouse.case_job", LeftEntity: "warehouse.cases", LeftField: "current_job_id", RightEntity: "rental.jobs", RightField: "job_id", Description: "Rental job currently assigned to a case."},
	{Name: "warehouse.task_job", LeftEntity: "warehouse.tasks", LeftField: "job_id", RightEntity: "rental.jobs", RightField: "job_id", Description: "Rental job driving a warehouse task."},
	{Name: "warehouse.task_product", LeftEntity: "warehouse.tasks", LeftField: "product_id", RightEntity: "warehouse.products", RightField: "product_id", Description: "Product handled by a warehouse task."},
	{Name: "planner.plan_tasks", LeftEntity: "planner.plans", LeftField: "plan_id", RightEntity: "planner.tasks", RightField: "plan_id", Description: "Tasks belonging to a PlannerCore plan."},
	{Name: "procurement.warehouse_product", LeftEntity: "warehouse.products", LeftField: "product_id", RightEntity: "procurement.products", RightField: "warehouse_product_id", Description: "Explicit product-master link between WarehouseCore and ProcurementCore."},
	{Name: "procurement.product_offers", LeftEntity: "procurement.products", LeftField: "procurement_product_id", RightEntity: "procurement.offers", RightField: "procurement_product_id", Description: "Supplier offers for a procurement product."},
	{Name: "procurement.product_requisition_lines", LeftEntity: "procurement.products", LeftField: "procurement_product_id", RightEntity: "procurement.requisition_lines", RightField: "procurement_product_id", Description: "Requisition demand for a procurement product."},
	{Name: "procurement.product_order_lines", LeftEntity: "procurement.products", LeftField: "procurement_product_id", RightEntity: "procurement.order_lines", RightField: "procurement_product_id", Description: "Purchase-order lines for a procurement product."},
	{Name: "procurement.requisition_lines", LeftEntity: "procurement.requisitions", LeftField: "requisition_id", RightEntity: "procurement.requisition_lines", RightField: "requisition_id", Description: "Lines belonging to a requisition."},
	{Name: "procurement.order_lines", LeftEntity: "procurement.orders", LeftField: "purchase_order_id", RightEntity: "procurement.order_lines", RightField: "purchase_order_id", Description: "Lines belonging to a purchase order."},
	{Name: "procurement.supplier_offers", LeftEntity: "procurement.suppliers", LeftField: "supplier_id", RightEntity: "procurement.offers", RightField: "supplier_id", Description: "Offers published by a supplier."},
	{Name: "procurement.supplier_orders", LeftEntity: "procurement.suppliers", LeftField: "supplier_id", RightEntity: "procurement.orders", RightField: "supplier_id", Description: "Purchase orders placed with a supplier."},
}

func registerQueryTools(server *mcp.Server, db *store.Store) {
	addTool(server, "cores.query.catalog", "Describe flexible Cores queries", "List every curated entity, safe field, field type, searchable field and supported cross-core relationship for cores.query.records and cores.query.aggregate.", func(_ context.Context, _ struct{}) (any, []Source, []string, error) {
		return queryCatalog(), []Source{{Service: "cores-mcp", Entity: "query_catalog"}}, nil, nil
	})

	addTool(server, "cores.query.records", "Query and join Cores records", "Run up to eight filtered, sorted and projected read-only queries across RentalCore, WarehouseCore, PlannerCore and ProcurementCore, then optionally join their results through documented relationships. Use this for combinations not covered by a dedicated business tool.", func(ctx context.Context, input QueryRecordsInput) (any, []Source, []string, error) {
		return executeRecordQueries(ctx, db, input)
	})

	addTool(server, "cores.query.aggregate", "Aggregate any curated Cores entity", "Group and aggregate a curated Cores entity with safe typed filters. Supports count, distinct count, sum, average, minimum and maximum without exposing arbitrary SQL.", func(ctx context.Context, input QueryAggregateInput) (any, []Source, []string, error) {
		spec, ok := queryEntities[input.Entity]
		if !ok {
			return nil, nil, nil, fmt.Errorf("unknown entity %q; call cores.query.catalog", input.Entity)
		}
		query, args, err := buildAggregateQuery(db, spec, input)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, query, args...)
		return rows, []Source{{Service: spec.Service, Entity: spec.SourceEntity}}, nil, err
	})
}

func queryCatalog() map[string]any {
	names := make([]string, 0, len(queryEntities))
	for name := range queryEntities {
		names = append(names, name)
	}
	sort.Strings(names)
	entities := make(map[string]any, len(names))
	for _, name := range names {
		spec := queryEntities[name]
		fields := make([]map[string]string, 0, len(spec.Fields))
		fieldNames := make([]string, 0, len(spec.Fields))
		for field := range spec.Fields {
			fieldNames = append(fieldNames, field)
		}
		sort.Strings(fieldNames)
		for _, field := range fieldNames {
			fields = append(fields, map[string]string{"name": field, "type": string(spec.Fields[field])})
		}
		entities[name] = map[string]any{"service": spec.Service, "fields": fields, "default_fields": spec.DefaultFields, "search_fields": spec.SearchFields}
	}
	relationships := make([]queryRelationship, len(queryRelationships))
	copy(relationships, queryRelationships)
	return map[string]any{
		"entities": entities, "relationships": relationships,
		"filter_operators":    []string{"eq", "ne", "contains", "not_contains", "prefix", "gt", "gte", "lt", "lte", "in", "not_in", "between", "is_null", "is_not_null"},
		"aggregate_functions": []string{"count", "count_distinct", "sum", "avg", "min", "max"},
		"limits":              map[string]int{"queries_per_call": 8, "joins_per_call": 8, "fields_per_query": 30, "filters_per_query": 20, "group_fields": 4, "metrics": 8},
	}
}

func executeRecordQueries(ctx context.Context, db *store.Store, input QueryRecordsInput) (any, []Source, []string, error) {
	if len(input.Queries) == 0 || len(input.Queries) > 8 {
		return nil, nil, nil, errorsNew("queries must contain between one and eight entries")
	}
	if len(input.Joins) > 8 {
		return nil, nil, nil, errorsNew("at most eight joins are allowed")
	}

	queries := make(map[string]EntityQueryInput, len(input.Queries))
	requiredFields := make(map[string]map[string]bool)
	for _, item := range input.Queries {
		if !queryAliasPattern.MatchString(item.Alias) {
			return nil, nil, nil, fmt.Errorf("invalid query alias %q", item.Alias)
		}
		if _, exists := queries[item.Alias]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate query alias %q", item.Alias)
		}
		if _, ok := queryEntities[item.Entity]; !ok {
			return nil, nil, nil, fmt.Errorf("unknown entity %q; call cores.query.catalog", item.Entity)
		}
		queries[item.Alias] = item
		requiredFields[item.Alias] = make(map[string]bool)
	}

	resolvedJoins := make([]QueryJoinInput, len(input.Joins))
	joinAliases := make(map[string]bool, len(input.Joins))
	for i, join := range input.Joins {
		resolved, err := resolveQueryJoin(join, queries)
		if err != nil {
			return nil, nil, nil, err
		}
		if joinAliases[resolved.Alias] {
			return nil, nil, nil, fmt.Errorf("duplicate join alias %q", resolved.Alias)
		}
		joinAliases[resolved.Alias] = true
		resolvedJoins[i] = resolved
		requiredFields[resolved.Left][resolved.LeftField] = true
		requiredFields[resolved.Right][resolved.RightField] = true
	}

	sections := make(map[string]querySection, len(input.Queries))
	sources := make([]Source, 0)
	warnings := make([]string, 0)
	for _, item := range input.Queries {
		spec := queryEntities[item.Entity]
		query, args, err := buildEntityQuery(db, spec, item, requiredFields[item.Alias])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("query %s: %w", item.Alias, err)
		}
		rows, err := db.Query(ctx, query, args...)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("query %s: %w", item.Alias, err)
		}
		sections[item.Alias] = querySection{Entity: item.Entity, Count: len(rows), Rows: rows}
		sources = append(sources, sourcesFor(spec.Service, spec.SourceEntity, rows)...)
		if len(rows) == db.Limit(item.Limit) {
			warnings = append(warnings, fmt.Sprintf("Query %s reached its row limit; refine filters or paginate with offset.", item.Alias))
		}
	}

	joined := make(map[string]joinedSection, len(resolvedJoins))
	for _, join := range resolvedJoins {
		section, err := joinQuerySections(db, join, sections)
		if err != nil {
			return nil, nil, nil, err
		}
		joined[join.Alias] = section
	}

	data := map[string]any{"queries": sections}
	if len(joined) > 0 {
		data["joins"] = joined
	}
	return data, deduplicateSources(sources), warnings, nil
}

func buildEntityQuery(db *store.Store, spec queryEntitySpec, input EntityQueryInput, required map[string]bool) (string, []any, error) {
	if len(input.Fields) > 30 || len(input.Filters) > 20 || len(input.Sort) > 8 {
		return "", nil, errorsNew("query exceeds field, filter, or sort limits")
	}
	fields := append([]string(nil), input.Fields...)
	if len(fields) == 0 {
		fields = append(fields, spec.DefaultFields...)
	}
	selected := make(map[string]bool, len(fields)+len(required))
	for _, field := range fields {
		if _, ok := spec.Fields[field]; !ok {
			return "", nil, fmt.Errorf("unknown field %q", field)
		}
		selected[field] = true
	}
	for field := range required {
		if _, ok := spec.Fields[field]; !ok {
			return "", nil, fmt.Errorf("unknown join field %q", field)
		}
		if !selected[field] {
			fields = append(fields, field)
			selected[field] = true
		}
	}
	if len(fields) == 0 {
		return "", nil, errorsNew("no fields selected")
	}

	selects := make([]string, 0, len(fields))
	for _, field := range fields {
		selects = append(selects, "q."+quoteQueryIdentifier(field))
	}
	where, args, err := buildQueryWhere(spec, input.Search, input.Filters)
	if err != nil {
		return "", nil, err
	}
	order, err := buildQuerySort(spec.Fields, input.Sort)
	if err != nil {
		return "", nil, err
	}
	if order == "" && len(spec.DefaultFields) > 0 {
		for _, candidate := range []string{"updated_at", "created_at", "start_date", "due_at", spec.DefaultFields[0]} {
			if _, ok := spec.Fields[candidate]; ok {
				order = " ORDER BY q." + quoteQueryIdentifier(candidate) + " DESC NULLS LAST"
				break
			}
		}
	}
	query := "SELECT " + strings.Join(selects, ",") + " FROM (" + spec.BaseSQL + ") q" + where + order
	args = append(args, db.Limit(input.Limit), cleanOffset(input.Offset))
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return query, args, nil
}

func buildQueryWhere(spec queryEntitySpec, search string, filters []QueryFilterInput) (string, []any, error) {
	clauses := make([]string, 0, len(filters)+1)
	args := make([]any, 0, len(filters)+1)
	if strings.TrimSpace(search) != "" {
		if len(spec.SearchFields) == 0 {
			return "", nil, errorsNew("entity does not support text search")
		}
		args = append(args, searchPattern(search))
		parts := make([]string, 0, len(spec.SearchFields))
		for _, field := range spec.SearchFields {
			parts = append(parts, fmt.Sprintf("COALESCE(q.%s::text,'') ILIKE $%d", quoteQueryIdentifier(field), len(args)))
		}
		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
	}
	for _, filter := range filters {
		kind, ok := spec.Fields[filter.Field]
		if !ok {
			return "", nil, fmt.Errorf("unknown filter field %q", filter.Field)
		}
		column := "q." + quoteQueryIdentifier(filter.Field)
		operator := strings.ToLower(strings.TrimSpace(filter.Operator))
		switch operator {
		case "is_null":
			clauses = append(clauses, column+" IS NULL")
		case "is_not_null":
			clauses = append(clauses, column+" IS NOT NULL")
		case "contains", "not_contains", "prefix":
			if kind != queryString {
				return "", nil, fmt.Errorf("operator %s requires a string field", operator)
			}
			value := filter.Value
			if operator == "contains" || operator == "not_contains" {
				value = "%" + value + "%"
			} else {
				value += "%"
			}
			args = append(args, value)
			not := ""
			if operator == "not_contains" {
				not = " NOT"
			}
			clauses = append(clauses, fmt.Sprintf("COALESCE(%s,'')%s ILIKE $%d", column, not, len(args)))
		case "eq", "ne", "gt", "gte", "lt", "lte":
			value, err := parseQueryValue(kind, filter.Value)
			if err != nil {
				return "", nil, fmt.Errorf("field %s: %w", filter.Field, err)
			}
			args = append(args, value)
			symbols := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
			clauses = append(clauses, fmt.Sprintf("%s %s $%d", column, symbols[operator], len(args)))
		case "in", "not_in":
			values := filter.Values
			if len(values) == 0 && strings.TrimSpace(filter.Value) != "" {
				values = strings.Split(filter.Value, ",")
			}
			if len(values) == 0 || len(values) > 50 {
				return "", nil, fmt.Errorf("operator %s requires between one and 50 values", operator)
			}
			placeholders := make([]string, 0, len(values))
			for _, raw := range values {
				value, err := parseQueryValue(kind, strings.TrimSpace(raw))
				if err != nil {
					return "", nil, fmt.Errorf("field %s: %w", filter.Field, err)
				}
				args = append(args, value)
				placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
			}
			not := ""
			if operator == "not_in" {
				not = " NOT"
			}
			clauses = append(clauses, fmt.Sprintf("%s%s IN (%s)", column, not, strings.Join(placeholders, ",")))
		case "between":
			if len(filter.Values) != 2 {
				return "", nil, errorsNew("between requires exactly two values")
			}
			low, err := parseQueryValue(kind, filter.Values[0])
			if err != nil {
				return "", nil, err
			}
			high, err := parseQueryValue(kind, filter.Values[1])
			if err != nil {
				return "", nil, err
			}
			args = append(args, low, high)
			clauses = append(clauses, fmt.Sprintf("%s BETWEEN $%d AND $%d", column, len(args)-1, len(args)))
		default:
			return "", nil, fmt.Errorf("unsupported operator %q", filter.Operator)
		}
	}
	if len(clauses) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func buildQuerySort(fields map[string]queryFieldKind, sorts []QuerySortInput) (string, error) {
	if len(sorts) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(sorts))
	for _, item := range sorts {
		if _, ok := fields[item.Field]; !ok {
			return "", fmt.Errorf("unknown sort field %q", item.Field)
		}
		direction := strings.ToUpper(strings.TrimSpace(item.Direction))
		if direction == "" {
			direction = "ASC"
		}
		if direction != "ASC" && direction != "DESC" {
			return "", fmt.Errorf("invalid sort direction %q", item.Direction)
		}
		nulls := strings.ToUpper(strings.TrimSpace(item.Nulls))
		if nulls != "" && nulls != "FIRST" && nulls != "LAST" {
			return "", fmt.Errorf("invalid null ordering %q", item.Nulls)
		}
		part := "q." + quoteQueryIdentifier(item.Field) + " " + direction
		if nulls != "" {
			part += " NULLS " + nulls
		}
		parts = append(parts, part)
	}
	return " ORDER BY " + strings.Join(parts, ","), nil
}

func parseQueryValue(kind queryFieldKind, raw string) (any, error) {
	switch kind {
	case queryString:
		return raw, nil
	case queryInteger:
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, errorsNew("expected an integer")
		}
		return value, nil
	case queryNumber:
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, errorsNew("expected a number")
		}
		return value, nil
	case queryBoolean:
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, errorsNew("expected true or false")
		}
		return value, nil
	case queryTime:
		value, err := parseDate(raw)
		if err != nil || value.IsZero() {
			return nil, errorsNew("expected YYYY-MM-DD or RFC3339")
		}
		return value, nil
	default:
		return nil, errorsNew("unsupported field type")
	}
}

func resolveQueryJoin(join QueryJoinInput, queries map[string]EntityQueryInput) (QueryJoinInput, error) {
	if !queryAliasPattern.MatchString(join.Alias) {
		return join, fmt.Errorf("invalid join alias %q", join.Alias)
	}
	left, leftOK := queries[join.Left]
	right, rightOK := queries[join.Right]
	if !leftOK || !rightOK {
		return join, fmt.Errorf("join %s references an unknown query alias", join.Alias)
	}
	if join.Relationship != "" {
		var relation *queryRelationship
		for i := range queryRelationships {
			if queryRelationships[i].Name == join.Relationship {
				relation = &queryRelationships[i]
				break
			}
		}
		if relation == nil {
			return join, fmt.Errorf("unknown relationship %q", join.Relationship)
		}
		switch {
		case left.Entity == relation.LeftEntity && right.Entity == relation.RightEntity:
			join.LeftField, join.RightField = relation.LeftField, relation.RightField
		case left.Entity == relation.RightEntity && right.Entity == relation.LeftEntity:
			join.LeftField, join.RightField = relation.RightField, relation.LeftField
		default:
			return join, fmt.Errorf("relationship %s does not connect %s and %s", join.Relationship, left.Entity, right.Entity)
		}
	}
	if _, ok := queryEntities[left.Entity].Fields[join.LeftField]; !ok {
		return join, fmt.Errorf("unknown left join field %q", join.LeftField)
	}
	if _, ok := queryEntities[right.Entity].Fields[join.RightField]; !ok {
		return join, fmt.Errorf("unknown right join field %q", join.RightField)
	}
	join.Type = strings.ToLower(strings.TrimSpace(join.Type))
	if join.Type == "" {
		join.Type = "inner"
	}
	if join.Type != "inner" && join.Type != "left" {
		return join, fmt.Errorf("join type must be inner or left")
	}
	return join, nil
}

func joinQuerySections(db *store.Store, join QueryJoinInput, sections map[string]querySection) (joinedSection, error) {
	left := sections[join.Left]
	right := sections[join.Right]
	index := make(map[string][]map[string]any)
	for _, row := range right.Rows {
		if key, ok := queryJoinKey(row[join.RightField]); ok {
			index[key] = append(index[key], row)
		}
	}
	limit := db.Limit(join.Limit)
	rows := make([]map[string]any, 0)
	for _, leftRow := range left.Rows {
		key, ok := queryJoinKey(leftRow[join.LeftField])
		matches := index[key]
		if !ok || len(matches) == 0 {
			if join.Type == "left" {
				rows = append(rows, map[string]any{join.Left: leftRow, join.Right: nil})
			}
		} else {
			for _, rightRow := range matches {
				rows = append(rows, map[string]any{join.Left: leftRow, join.Right: rightRow})
				if len(rows) >= limit {
					break
				}
			}
		}
		if len(rows) >= limit {
			break
		}
	}
	return joinedSection{Left: join.Left, Right: join.Right, Relationship: join.Relationship, Count: len(rows), Rows: rows}, nil
}

func queryJoinKey(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	return fmt.Sprint(value), true
}

func buildAggregateQuery(db *store.Store, spec queryEntitySpec, input QueryAggregateInput) (string, []any, error) {
	if len(input.GroupBy) > 4 || len(input.Metrics) > 8 || len(input.Filters) > 20 || len(input.Sort) > 8 {
		return "", nil, errorsNew("aggregate query exceeds group, metric, filter, or sort limits")
	}
	selects := make([]string, 0, len(input.GroupBy)+len(input.Metrics)+1)
	groups := make([]string, 0, len(input.GroupBy))
	outputFields := make(map[string]queryFieldKind)
	for _, field := range input.GroupBy {
		kind, ok := spec.Fields[field]
		if !ok {
			return "", nil, fmt.Errorf("unknown group field %q", field)
		}
		column := "q." + quoteQueryIdentifier(field)
		selects = append(selects, column)
		groups = append(groups, column)
		outputFields[field] = kind
	}
	metrics := input.Metrics
	if len(metrics) == 0 {
		metrics = []QueryAggregateMetric{{Function: "count", Alias: "record_count"}}
	}
	for _, metric := range metrics {
		function := strings.ToLower(strings.TrimSpace(metric.Function))
		if function != "count" && function != "count_distinct" && function != "sum" && function != "avg" && function != "min" && function != "max" {
			return "", nil, fmt.Errorf("unsupported aggregate function %q", metric.Function)
		}
		fieldKind, hasField := spec.Fields[metric.Field]
		if metric.Field != "" && !hasField {
			return "", nil, fmt.Errorf("unknown aggregate field %q", metric.Field)
		}
		if function != "count" && !hasField {
			return "", nil, fmt.Errorf("aggregate %s requires a documented field", function)
		}
		if (function == "sum" || function == "avg") && fieldKind != queryInteger && fieldKind != queryNumber {
			return "", nil, fmt.Errorf("aggregate %s requires a numeric field", function)
		}
		alias := metric.Alias
		if alias == "" {
			alias = function
			if metric.Field != "" {
				alias += "_" + metric.Field
			} else {
				alias += "_rows"
			}
		}
		if !queryAliasPattern.MatchString(alias) {
			return "", nil, fmt.Errorf("invalid aggregate alias %q", alias)
		}
		if _, exists := outputFields[alias]; exists {
			return "", nil, fmt.Errorf("duplicate aggregate output %q", alias)
		}
		expression := "*"
		if metric.Field != "" {
			expression = "q." + quoteQueryIdentifier(metric.Field)
		}
		sqlFunction := strings.ToUpper(function)
		if function == "count_distinct" {
			sqlFunction, expression = "COUNT", "DISTINCT "+expression
		}
		selects = append(selects, fmt.Sprintf("%s(%s) AS %s", sqlFunction, expression, quoteQueryIdentifier(alias)))
		outputFields[alias] = queryNumber
	}
	where, args, err := buildQueryWhere(spec, input.Search, input.Filters)
	if err != nil {
		return "", nil, err
	}
	order, err := buildQuerySort(outputFields, input.Sort)
	if err != nil {
		return "", nil, err
	}
	if order == "" && len(metrics) > 0 {
		alias := metrics[0].Alias
		if alias == "" {
			alias = strings.ToLower(strings.TrimSpace(metrics[0].Function))
			if metrics[0].Field != "" {
				alias += "_" + metrics[0].Field
			} else {
				alias += "_rows"
			}
		}
		order = " ORDER BY q." + quoteQueryIdentifier(alias) + " DESC"
		// Aggregate aliases belong to the SELECT scope, not the source alias.
		order = strings.Replace(order, "q."+quoteQueryIdentifier(alias), quoteQueryIdentifier(alias), 1)
	} else if order != "" {
		for field := range outputFields {
			if _, sourceField := spec.Fields[field]; !sourceField {
				order = strings.ReplaceAll(order, "q."+quoteQueryIdentifier(field), quoteQueryIdentifier(field))
			}
		}
	}
	query := "SELECT " + strings.Join(selects, ",") + " FROM (" + spec.BaseSQL + ") q" + where
	if len(groups) > 0 {
		query += " GROUP BY " + strings.Join(groups, ",")
	}
	query += order
	args = append(args, db.Limit(input.Limit))
	query += fmt.Sprintf(" LIMIT $%d", len(args))
	return query, args, nil
}

func quoteQueryIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func deduplicateSources(input []Source) []Source {
	result := make([]Source, 0, len(input))
	seen := make(map[string]bool)
	for _, source := range input {
		key := source.Service + "\x00" + source.Entity + "\x00" + source.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, source)
		if len(result) >= 200 {
			break
		}
	}
	return result
}

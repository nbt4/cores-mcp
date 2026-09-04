package mcpserver

import (
	"context"
	"math"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

func registerCrossCoreTools(server *mcp.Server, db *store.Store) {
	addTool(server, "inventory.coverage.check", "Check inventory coverage", "Determine whether matching products cover peak concurrent demand from upcoming rental jobs, including available stock, blocked units, safety stock, shortages and procurement links. This is the primary tool for questions such as whether enough couplers are available.", func(ctx context.Context, input ProductWindowInput) (any, []Source, []string, error) {
		from, to, err := dateWindow(input.From, input.To)
		if err != nil {
			return nil, nil, nil, err
		}
		safety := input.SafetyStockPercent
		if safety == 0 {
			safety = 10
		}
		safety = math.Max(0, math.Min(100, safety))
		rows, err := db.Query(ctx, `WITH selected AS (
                SELECT p.productid,p.product_code,p.name,p.tracking_mode,p.stock_quantity,p.min_stock_level,p.categoryid
                  FROM products p LEFT JOIN categories c ON c.categoryid=p.categoryid LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid
                 WHERE COALESCE(p.lifecycle_status,'active')='active' AND (p.name ILIKE $1 OR p.product_code ILIKE $1 OR p.description ILIKE $1 OR p.model_number ILIKE $1 OR c.name ILIKE $1 OR m.name ILIKE $1)
                 ORDER BY p.name LIMIT $5
              ), stock AS (
                SELECT s.productid,
                       (SELECT count(*) FROM devices d WHERE d.productid=s.productid) AS total_devices,
                       (SELECT count(*) FROM devices d WHERE d.productid=s.productid AND d.condition_status='available') AS available_devices,
                       (SELECT count(*) FROM devices d WHERE d.productid=s.productid AND d.condition_status<>'available') AS blocked_devices,
                       (SELECT COALESCE(sum(pl.quantity),0) FROM product_locations pl WHERE pl.product_id=s.productid) AS located_quantity
                  FROM selected s
              ), daily AS (
                SELECT r.product_id,day::date AS demand_date,sum(r.quantity) AS demand
                  FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id
                  CROSS JOIN LATERAL generate_series(GREATEST(j.startdate,$2::date),LEAST(COALESCE(j.enddate,j.startdate),$3::date),interval '1 day') day
                 WHERE j.deleted_at IS NULL AND j.startdate<=$3 AND COALESCE(j.enddate,j.startdate)>=$2 AND r.product_id IN (SELECT productid FROM selected)
                 GROUP BY r.product_id,day::date
              ), demand AS (
                SELECT product_id,max(demand) AS peak_demand,min(demand_date) FILTER (WHERE demand=(SELECT max(d2.demand) FROM daily d2 WHERE d2.product_id=daily.product_id)) AS peak_date FROM daily GROUP BY product_id
              )
              SELECT s.productid AS product_id,s.product_code,s.name,s.tracking_mode,
                     CASE WHEN s.tracking_mode='individual' THEN COALESCE(st.available_devices,0) ELSE COALESCE(s.stock_quantity,st.located_quantity,0) END AS available,
                     COALESCE(st.total_devices,0) AS total_devices,COALESCE(st.blocked_devices,0) AS blocked_devices,COALESCE(st.located_quantity,0) AS located_quantity,
                     COALESCE(d.peak_demand,0) AS peak_concurrent_demand,d.peak_date,$4::numeric AS safety_stock_percent,
                     ceil(COALESCE(d.peak_demand,0)*(1+$4::numeric/100)) AS demand_with_safety_stock,
                     GREATEST(ceil(COALESCE(d.peak_demand,0)*(1+$4::numeric/100))-(CASE WHEN s.tracking_mode='individual' THEN COALESCE(st.available_devices,0) ELSE COALESCE(s.stock_quantity,st.located_quantity,0) END),0) AS shortage,
                     pp.id AS procurement_product_id,pp.sku,pp.reorder_point,pp.target_stock
                FROM selected s LEFT JOIN stock st ON st.productid=s.productid LEFT JOIN demand d ON d.product_id=s.productid
                LEFT JOIN core_product_links cpl ON cpl.warehouse_product_id=s.productid LEFT JOIN proc_products pp ON pp.id=cpl.procurement_product_id
               ORDER BY shortage DESC,s.name`, searchPattern(input.Query), from, to, safety, db.Limit(input.Limit))
		warnings := []string{"Coverage uses peak concurrent quantities from explicit job product requirements. Unexpanded package contents or incomplete job requirements can cause understated demand.", "A zero shortage means recorded demand is covered; it is not a structural or rigging safety approval."}
		return rows, sourcesFor("warehousecore", "product", rows), warnings, err
	})

	addTool(server, "inventory.alternatives.compare", "Compare inventory alternatives", "Compare matching products and their typed alternatives or accessories using available stock, upcoming peak demand, defects, maintenance, usage and procurement offers.", func(ctx context.Context, input ProductWindowInput) (any, []Source, []string, error) {
		from, to, err := dateWindow(input.From, input.To)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `WITH matched AS (
                SELECT productid FROM products WHERE name ILIKE $1 OR product_code ILIKE $1 OR description ILIKE $1
              ), candidates AS (
                SELECT productid FROM matched UNION SELECT dependency_product_id FROM product_dependencies WHERE product_id IN (SELECT productid FROM matched)
                UNION SELECT product_id FROM product_dependencies WHERE dependency_product_id IN (SELECT productid FROM matched)
              ), metrics AS (
                SELECT p.productid,p.product_code,p.name,p.tracking_mode,p.stock_quantity,p.min_stock_level,p.price_per_unit,p.itemcostperday,p.weight,p.attributes,
                       dm.devices,dm.available_devices,dm.usage_hours,df.open_defects,mm.open_maintenance
                  FROM products p
                  LEFT JOIN LATERAL (SELECT count(*) AS devices,count(*) FILTER (WHERE condition_status='available') AS available_devices,
                                            COALESCE(sum(usage_hours),0) AS usage_hours FROM devices d WHERE d.productid=p.productid) dm ON true
                  LEFT JOIN LATERAL (SELECT count(*) AS open_defects FROM defect_reports dr JOIN devices d ON d.deviceid=dr.device_id
                                      WHERE d.productid=p.productid AND lower(COALESCE(dr.status,'')) NOT IN ('resolved','closed','done')) df ON true
                  LEFT JOIN LATERAL (SELECT count(*) AS open_maintenance FROM maintenance_orders mo JOIN devices d ON d.deviceid=mo.device_id
                                      WHERE d.productid=p.productid AND lower(mo.status) NOT IN ('completed','closed','cancelled')) mm ON true
                 WHERE p.productid IN (SELECT productid FROM candidates)
              )
              SELECT m.*,COALESCE(dem.peak_demand,0) AS peak_demand,rel.relation_type,rel.assignment_scope,rel.is_optional,rel.default_quantity,
                     offer.supplier,offer.price_cents AS best_price_cents,offer.currency,offer.lead_days,offer.last_checked_at
                FROM metrics m
                LEFT JOIN LATERAL (SELECT max(day_demand) AS peak_demand FROM (SELECT day::date,sum(r.quantity) AS day_demand FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id CROSS JOIN LATERAL generate_series(GREATEST(j.startdate,$2::date),LEAST(COALESCE(j.enddate,j.startdate),$3::date),interval '1 day') day WHERE r.product_id=m.productid AND j.deleted_at IS NULL AND j.startdate<=$3 AND COALESCE(j.enddate,j.startdate)>=$2 GROUP BY day::date) q) dem ON true
                LEFT JOIN LATERAL (SELECT pd.relation_type,pd.assignment_scope,pd.is_optional,pd.default_quantity FROM product_dependencies pd WHERE (pd.product_id IN (SELECT productid FROM matched) AND pd.dependency_product_id=m.productid) OR (pd.dependency_product_id IN (SELECT productid FROM matched) AND pd.product_id=m.productid) LIMIT 1) rel ON true
                LEFT JOIN core_product_links cpl ON cpl.warehouse_product_id=m.productid LEFT JOIN proc_products pp ON pp.id=cpl.procurement_product_id
                LEFT JOIN LATERAL (SELECT s.name AS supplier,o.price_cents,o.currency,o.lead_days,o.last_checked_at FROM proc_offers o JOIN proc_suppliers s ON s.id=o.supplier_id WHERE o.product_id=pp.id AND o.active=true ORDER BY o.price_cents/NULLIF(o.pack_size,0),o.lead_days LIMIT 1) offer ON true
               ORDER BY m.name LIMIT $4`, searchPattern(input.Query), from, to, db.Limit(input.Limit))
		return rows, sourcesFor("warehousecore", "product", rows), []string{"Product relations express recorded compatibility, not engineering approval. Verify load limits and safety-critical use with manufacturer documentation and a qualified person."}, err
	})

	rowsTool(server, db, "equipment.strategy.context", "Build equipment strategy context", "Return a factual portfolio view for an equipment category or manufacturer: assets, availability, age, usage, revenue, defects, maintenance cost, purchasing history, prices and supplier options. Useful for platform decisions such as selecting a battery ecosystem.", "cores", "equipment_strategy", func(input SearchInput) (string, []any) {
		return `SELECT p.productid AS warehouse_product_id,p.product_code,p.name,m.name AS manufacturer,b.name AS brand,p.model_number,p.tracking_mode,
                       dm.assets,dm.available_assets,dm.average_age_years,dm.usage_hours,dm.asset_revenue,dm.recorded_maintenance_cost,
                       df.defects,df.open_defects,
                       pp.id AS procurement_product_id,pp.sku,pp.reorder_point,pp.target_stock,
                       pm.purchase_orders,pm.ordered_quantity,om.best_current_price_cents,om.price_checked_at,om.supplier_options
                  FROM products p LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid LEFT JOIN brands b ON b.brandid=p.brandid
                  LEFT JOIN core_product_links cpl ON cpl.warehouse_product_id=p.productid LEFT JOIN proc_products pp ON pp.id=cpl.procurement_product_id
                  LEFT JOIN LATERAL (SELECT count(*) AS assets,count(*) FILTER (WHERE condition_status='available') AS available_assets,
                                            round(avg(EXTRACT(year FROM age(current_date,purchasedate))),1) AS average_age_years,
                                            round(COALESCE(sum(usage_hours),0),1) AS usage_hours,round(COALESCE(sum(total_revenue),0),2) AS asset_revenue,
                                            round(COALESCE(sum(last_maintenance_cost),0),2) AS recorded_maintenance_cost FROM devices d WHERE d.productid=p.productid) dm ON true
                  LEFT JOIN LATERAL (SELECT count(*) AS defects,count(*) FILTER (WHERE lower(COALESCE(dr.status,'')) NOT IN ('resolved','closed','done')) AS open_defects
                                       FROM defect_reports dr JOIN devices d ON d.deviceid=dr.device_id WHERE d.productid=p.productid) df ON true
                  LEFT JOIN LATERAL (SELECT count(DISTINCT po.id) AS purchase_orders,COALESCE(sum(pol.quantity),0) AS ordered_quantity
                                       FROM proc_purchase_order_lines pol JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id WHERE pol.product_id=pp.id) pm ON true
                  LEFT JOIN LATERAL (SELECT min(price_cents) FILTER (WHERE active) AS best_current_price_cents,max(last_checked_at) AS price_checked_at,
                                            count(DISTINCT supplier_id) FILTER (WHERE active) AS supplier_options FROM proc_offers o WHERE o.product_id=pp.id) om ON true
                 WHERE $1='' OR p.name ILIKE $2 OR p.description ILIKE $2 OR p.attributes::text ILIKE $2 OR m.name ILIKE $2 OR b.name ILIKE $2 OR pp.name ILIKE $2 OR pp.manufacturer ILIKE $2 OR pp.attributes::text ILIKE $2
                 ORDER BY dm.assets DESC,p.name LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})

	addTool(server, "inventory.procurement.recommendations", "Generate factual replenishment candidates", "Return products with shortages against future peak demand or configured stock targets, enriched with alternatives, open requisitions, inbound orders and best supplier offer. The tool returns facts, not an automatic purchase decision.", func(ctx context.Context, input ProductWindowInput) (any, []Source, []string, error) {
		from, to, err := dateWindow(input.From, input.To)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err := db.Query(ctx, `WITH demand AS (
                SELECT product_id,max(day_demand) AS peak_demand FROM (SELECT r.product_id,day::date,sum(r.quantity) AS day_demand
                  FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id CROSS JOIN LATERAL generate_series(GREATEST(j.startdate,$2::date),LEAST(COALESCE(j.enddate,j.startdate),$3::date),interval '1 day') day
                 WHERE j.deleted_at IS NULL AND j.startdate<=$3 AND COALESCE(j.enddate,j.startdate)>=$2 GROUP BY r.product_id,day::date) q GROUP BY product_id
              ), metrics AS (
                SELECT p.productid,p.product_code,p.name,p.tracking_mode,p.stock_quantity,p.min_stock_level,count(d.deviceid) FILTER (WHERE d.condition_status='available') AS available_devices
                  FROM products p LEFT JOIN devices d ON d.productid=p.productid WHERE COALESCE(p.lifecycle_status,'active')='active' GROUP BY p.productid
              )
              SELECT m.productid AS warehouse_product_id,m.product_code,m.name,m.tracking_mode,
                     CASE WHEN m.tracking_mode='individual' THEN COALESCE(m.available_devices,0) ELSE COALESCE(m.stock_quantity,0) END AS available,
                     COALESCE(d.peak_demand,0) AS peak_demand,m.min_stock_level,
                     GREATEST(GREATEST(COALESCE(d.peak_demand,0),COALESCE(m.min_stock_level,0))-(CASE WHEN m.tracking_mode='individual' THEN COALESCE(m.available_devices,0) ELSE COALESCE(m.stock_quantity,0) END),0) AS suggested_quantity,
                     pp.id AS procurement_product_id,pp.sku,pp.target_stock,
                     (SELECT count(*) FROM proc_requisition_lines rl JOIN proc_requisitions r ON r.id=rl.requisition_id WHERE rl.product_id=pp.id AND lower(r.status) NOT IN ('ordered','rejected','cancelled','closed')) AS open_requisitions,
                     (SELECT COALESCE(sum(pol.quantity-pol.received_quantity),0) FROM proc_purchase_order_lines pol JOIN proc_purchase_orders po ON po.id=pol.purchase_order_id WHERE pol.product_id=pp.id AND lower(po.status) NOT IN ('received','completed','cancelled','closed')) AS inbound_quantity,
                     offer.supplier,offer.price_cents,offer.currency,offer.lead_days,offer.last_checked_at
                FROM metrics m LEFT JOIN demand d ON d.product_id=m.productid LEFT JOIN core_product_links cpl ON cpl.warehouse_product_id=m.productid
                LEFT JOIN proc_products pp ON pp.id=cpl.procurement_product_id
                LEFT JOIN LATERAL (SELECT s.name AS supplier,o.price_cents,o.currency,o.lead_days,o.last_checked_at FROM proc_offers o JOIN proc_suppliers s ON s.id=o.supplier_id WHERE o.product_id=pp.id AND o.active=true AND s.active=true ORDER BY o.price_cents/NULLIF(o.pack_size,0),o.lead_days LIMIT 1) offer ON true
               WHERE ($1='' OR m.name ILIKE $4 OR m.product_code ILIKE $4) AND GREATEST(COALESCE(d.peak_demand,0),COALESCE(m.min_stock_level,0))>(CASE WHEN m.tracking_mode='individual' THEN COALESCE(m.available_devices,0) ELSE COALESCE(m.stock_quantity,0) END)
               ORDER BY suggested_quantity DESC,m.name LIMIT $5`, input.Query, from, to, searchPattern(input.Query), db.Limit(input.Limit))
		return rows, sourcesFor("warehousecore", "product", rows), []string{"Suggested quantities do not subtract unexpanded package demand and should be reviewed before procurement."}, err
	})

	addTool(server, "cores.data.quality", "Find decision-relevant data quality gaps", "Identify missing or inconsistent product, device, job, location, procurement and planner data that can weaken AI analysis.", func(ctx context.Context, _ struct{}) (any, []Source, []string, error) {
		rows, err := db.Query(ctx, `SELECT * FROM (
                SELECT 'product_missing_tracking_mode' AS issue,count(*) AS affected FROM products WHERE tracking_mode IS NULL OR tracking_mode=''
                UNION ALL SELECT 'product_missing_code',count(*) FROM products WHERE product_code IS NULL OR product_code=''
                UNION ALL SELECT 'product_missing_category',count(*) FROM products WHERE categoryid IS NULL
                UNION ALL SELECT 'bulk_product_missing_stock',count(*) FROM products WHERE COALESCE(tracking_mode,'')<>'individual' AND stock_quantity IS NULL
                UNION ALL SELECT 'product_missing_minimum_stock',count(*) FROM products WHERE min_stock_level IS NULL
                UNION ALL SELECT 'device_missing_location',count(*) FROM devices WHERE zone_id IS NULL AND current_case_id IS NULL
                UNION ALL SELECT 'device_missing_condition',count(*) FROM devices WHERE condition_status IS NULL OR condition_status=''
                UNION ALL SELECT 'future_job_without_requirements',count(*) FROM jobs j WHERE j.deleted_at IS NULL AND j.startdate>=current_date AND NOT EXISTS (SELECT 1 FROM job_product_requirements r WHERE r.job_id=j.jobid)
                UNION ALL SELECT 'procurement_product_not_linked_to_warehouse',count(*) FROM proc_products p WHERE p.active=true AND NOT EXISTS (SELECT 1 FROM core_product_links l WHERE l.procurement_product_id=p.id)
                UNION ALL SELECT 'warehouse_product_not_linked_to_procurement',count(*) FROM products p WHERE COALESCE(p.lifecycle_status,'active')='active' AND NOT EXISTS (SELECT 1 FROM core_product_links l WHERE l.warehouse_product_id=p.productid)
                UNION ALL SELECT 'active_offer_without_recent_check_30d',count(*) FROM proc_offers WHERE active=true AND (last_checked_at IS NULL OR last_checked_at<now()-interval '30 days')
                UNION ALL SELECT 'open_planner_task_without_due_date',count(*) FROM planner_tasks t JOIN planner_plans p ON p.id=t.plan_id WHERE p.archived_at IS NULL AND t.completed_at IS NULL AND t.progress<100 AND t.due_date IS NULL
              ) quality ORDER BY affected DESC,issue`)
		return rows, []Source{{Service: "cores", Entity: "data_quality"}}, []string{"These checks flag missing fields, not necessarily business errors."}, err
	})

	rowsTool(server, db, "warehouse.utilization.summary", "Summarize equipment utilization", "Summarize historic and upcoming use, asset revenue, condition and defects by warehouse product.", "warehousecore", "product", func(input SearchInput) (string, []any) {
		return `SELECT p.productid AS product_id,p.product_code,p.name,dm.devices,jm.historic_job_uses,jm.future_job_uses,
                       dm.usage_hours,dm.asset_revenue,dm.unavailable_devices,df.defects,jm.latest_job
                  FROM products p
                  LEFT JOIN LATERAL (SELECT count(*) AS devices,round(COALESCE(sum(usage_hours),0),1) AS usage_hours,
                                            round(COALESCE(sum(total_revenue),0),2) AS asset_revenue,
                                            count(*) FILTER (WHERE condition_status<>'available') AS unavailable_devices FROM devices d WHERE d.productid=p.productid) dm ON true
                  LEFT JOIN LATERAL (SELECT count(DISTINCT jd.jobid) AS historic_job_uses,count(DISTINCT jd.jobid) FILTER (WHERE j.startdate>=current_date) AS future_job_uses,
                                            max(j.startdate) AS latest_job FROM devices d JOIN job_devices jd ON jd.deviceid=d.deviceid JOIN jobs j ON j.jobid=jd.jobid AND j.deleted_at IS NULL
                                      WHERE d.productid=p.productid) jm ON true
                  LEFT JOIN LATERAL (SELECT count(*) AS defects FROM devices d JOIN defect_reports dr ON dr.device_id=d.deviceid WHERE d.productid=p.productid) df ON true
                 WHERE $1='' OR p.name ILIKE $2 OR p.product_code ILIKE $2 OR p.description ILIKE $2
		         ORDER BY jm.historic_job_uses DESC,p.name LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})
}

var _ = time.RFC3339

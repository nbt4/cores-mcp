package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

func registerSuiteTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	addTool(server, "cores.operations.overview", "Get Cores operations overview", "Return a cross-suite operational snapshot covering upcoming jobs, stock risks, defects, maintenance, warehouse work, planner work and procurement.", func(ctx context.Context, _ struct{}) (any, []Source, []string, error) {
		rows, err := db.Query(ctx, `SELECT
              (SELECT count(*) FROM jobs WHERE deleted_at IS NULL AND startdate>=current_date AND startdate<current_date+interval '90 days') AS upcoming_jobs,
              (SELECT count(*) FROM devices) AS devices,
              (SELECT count(*) FROM devices WHERE condition_status<>'available') AS unavailable_devices,
              (SELECT count(*) FROM defect_reports WHERE lower(COALESCE(status,'')) NOT IN ('resolved','closed','done')) AS open_defects,
              (SELECT count(*) FROM maintenance_orders WHERE lower(status) NOT IN ('completed','closed','cancelled') AND due_at<now()) AS overdue_maintenance,
              (SELECT count(*) FROM warehouse_tasks WHERE lower(status) NOT IN ('completed','done','cancelled','closed')) AS open_warehouse_tasks,
              (SELECT count(*) FROM planner_tasks t JOIN planner_plans p ON p.id=t.plan_id WHERE EXISTS (SELECT 1 FROM planner_members access_member WHERE access_member.plan_id=p.id AND access_member.user_id=current_setting('cores.user_id', true)) AND p.archived_at IS NULL AND t.completed_at IS NULL AND t.progress<100) AS open_planner_tasks,
              (SELECT count(*) FROM planner_tasks t JOIN planner_plans p ON p.id=t.plan_id WHERE EXISTS (SELECT 1 FROM planner_members access_member WHERE access_member.plan_id=p.id AND access_member.user_id=current_setting('cores.user_id', true)) AND p.archived_at IS NULL AND t.completed_at IS NULL AND t.progress<100 AND t.due_date<now()) AS overdue_planner_tasks,
              (SELECT count(*) FROM proc_requisitions WHERE lower(status) NOT IN ('ordered','approved','rejected','cancelled','closed')) AS open_requisitions,
              (SELECT count(*) FROM proc_purchase_orders WHERE lower(status) NOT IN ('received','completed','cancelled','closed')) AS open_purchase_orders,
              (SELECT count(*) FROM proc_purchase_orders WHERE expected_delivery<now() AND lower(status) NOT IN ('received','completed','cancelled','closed')) AS overdue_deliveries`)
		return firstRow(rows), []Source{{Service: "cores", Entity: "operations"}}, nil, err
	})

	rowsTool(server, db, "cores.search", "Search the complete Cores suite", "Search products, devices, jobs, planner tasks, procurement products, suppliers, requisitions, purchase orders, venues and cases from one tool.", "cores", "search", func(input SearchInput) (string, []any) {
		return `SELECT * FROM (
                SELECT 'warehouse_product' AS entity_type,p.productid::text AS id,p.name AS title,concat_ws(' · ',p.product_code,c.name,m.name,p.model_number) AS context,p.updated_at AS updated_at FROM products p LEFT JOIN categories c ON c.categoryid=p.categoryid LEFT JOIN manufacturer m ON m.manufacturerid=p.manufacturerid WHERE p.name ILIKE $1 OR p.product_code ILIKE $1 OR p.description ILIKE $1 OR p.model_number ILIKE $1
                UNION ALL SELECT 'device',d.deviceid,d.deviceid,concat_ws(' · ',p.name,d.serialnumber,d.barcode,d.status,d.condition_status),d.updated_at FROM devices d JOIN products p ON p.productid=d.productid WHERE d.deviceid ILIKE $1 OR d.serialnumber ILIKE $1 OR d.barcode ILIKE $1 OR p.name ILIKE $1
                UNION ALL SELECT 'rental_job',j.jobid::text,COALESCE(NULLIF(j.description,''),j.job_code),concat_ws(' · ',j.job_code,s.status,j.startdate::text),j.updated_at FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid WHERE j.deleted_at IS NULL AND (j.job_code ILIKE $1 OR j.description ILIKE $1)
                UNION ALL SELECT 'planner_task',t.id::text,t.title,concat_ws(' · ',p.name,b.name,t.priority,t.progress::text),t.updated_at FROM planner_tasks t JOIN planner_plans p ON p.id=t.plan_id LEFT JOIN planner_buckets b ON b.id=t.bucket_id WHERE EXISTS (SELECT 1 FROM planner_members access_member WHERE access_member.plan_id=p.id AND access_member.user_id=current_setting('cores.user_id', true)) AND p.archived_at IS NULL AND (t.title ILIKE $1 OR t.rich_text_notes ILIKE $1 OR p.name ILIKE $1)
                UNION ALL SELECT 'procurement_product',p.id::text,p.name,concat_ws(' · ',p.sku,p.manufacturer,p.model),p.updated_at FROM proc_products p WHERE p.active=true AND (p.name ILIKE $1 OR p.sku ILIKE $1 OR p.description ILIKE $1 OR p.manufacturer ILIKE $1 OR p.model ILIKE $1)
                UNION ALL SELECT 'supplier',s.id::text,s.name,concat_ws(' · ',s.code,s.risk_level,s.rating::text),s.updated_at FROM proc_suppliers s WHERE s.active=true AND (s.name ILIKE $1 OR s.code ILIKE $1)
                UNION ALL SELECT 'requisition',r.id::text,r.title,concat_ws(' · ',r.number,r.status,r.requester_name),r.updated_at FROM proc_requisitions r WHERE r.title ILIKE $1 OR r.number ILIKE $1 OR r.justification ILIKE $1
                UNION ALL SELECT 'purchase_order',po.id::text,po.number,concat_ws(' · ',s.name,po.status,po.expected_delivery::text),po.updated_at FROM proc_purchase_orders po LEFT JOIN proc_suppliers s ON s.id=po.supplier_id WHERE po.number ILIKE $1 OR s.name ILIKE $1
				UNION ALL SELECT 'venue',v.id::text,v.name,concat_ws(' · ',v.city,v.zip),v.updated_at FROM venues v WHERE v.name ILIKE $1 OR v.city ILIKE $1
                UNION ALL SELECT 'case',c.caseid::text,c.name,concat_ws(' · ',c.barcode,c.workflow_status,z.name),c.updated_at FROM cases c LEFT JOIN storage_zones z ON z.zone_id=c.zone_id WHERE c.name ILIKE $1 OR c.barcode ILIKE $1
              ) results ORDER BY updated_at DESC NULLS LAST LIMIT $2 OFFSET $3`, []any{searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})

	windowRowsTool(server, db, "cores.activity.recent", "List recent cross-suite activity", "Return recent operational changes from jobs, devices, planner tasks, procurement activities, movements, defects and maintenance.", "cores", "activity", func(input WindowInput, from, to time.Time) (string, []any) {
		return `SELECT * FROM (
				SELECT 'rental_job' AS source,j.jobid::text AS id,COALESCE(NULLIF(j.description,''),j.job_code) AS subject,'updated' AS action,j.updated_at AS occurred_at FROM jobs j WHERE j.deleted_at IS NULL AND j.updated_at >= $1 AND j.updated_at < $2::timestamp+interval '1 day'
				UNION ALL SELECT 'device',d.deviceid,p.name,concat('status=',d.status,', condition=',d.condition_status),d.updated_at FROM devices d JOIN products p ON p.productid=d.productid WHERE d.updated_at >= $1 AND d.updated_at < $2::timestamp+interval '1 day'
				UNION ALL SELECT 'planner_task',t.id::text,t.title,concat('progress=',t.progress),t.updated_at FROM planner_tasks t WHERE EXISTS (SELECT 1 FROM planner_members access_member WHERE access_member.plan_id=t.plan_id AND access_member.user_id=current_setting('cores.user_id', true)) AND t.updated_at >= $1 AND t.updated_at < $2::timestamptz+interval '1 day'
				UNION ALL SELECT 'procurement',a.entity_id::text,concat(a.entity_type,' ',a.entity_id),a.action,a.created_at FROM proc_activities a WHERE a.created_at >= $1 AND a.created_at < $2::timestamptz+interval '1 day'
				UNION ALL SELECT 'movement',m.movement_id::text,p.name,m.movement_type,m.created_at FROM device_movements m JOIN devices d ON d.deviceid=m.device_id JOIN products p ON p.productid=d.productid WHERE m.created_at >= $1 AND m.created_at < $2::timestamp+interval '1 day'
				UNION ALL SELECT 'defect',d.defect_id::text,p.name,concat(d.severity,' ',d.status),d.updated_at FROM defect_reports d JOIN devices dv ON dv.deviceid=d.device_id JOIN products p ON p.productid=dv.productid WHERE d.updated_at >= $1 AND d.updated_at < $2::timestamp+interval '1 day'
				UNION ALL SELECT 'maintenance',m.order_id::text,m.title,concat(m.priority,' ',m.status),m.updated_at FROM maintenance_orders m WHERE m.updated_at >= $1 AND m.updated_at < $2::timestamp+interval '1 day'
              ) activity ORDER BY occurred_at DESC LIMIT $3`, []any{from, to, db.Limit(input.Limit)}
	})

	addTool(server, "cores.services.health", "Check Cores service health", "Check live health endpoints for all Cores services. This reads operational endpoints only and never changes service state.", func(ctx context.Context, _ struct{}) (any, []Source, []string, error) {
		services := map[string]string{"rentalcore": cfg.RentalURL + "/health", "warehousecore": cfg.WarehouseURL + "/api/v1/health", "plannercore": cfg.PlannerURL + "/health", "procurementcore": cfg.ProcurementURL + "/health"}
		results := make(map[string]any, len(services))
		var mu sync.Mutex
		var wg sync.WaitGroup
		client := &http.Client{Timeout: 5 * time.Second}
		for name, endpoint := range services {
			name, endpoint := name, endpoint
			wg.Add(1)
			go func() {
				defer wg.Done()
				started := time.Now()
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
				response, err := client.Do(request)
				result := map[string]any{"healthy": false, "latency_ms": time.Since(started).Milliseconds()}
				if err != nil {
					result["error"] = err.Error()
				} else {
					defer response.Body.Close()
					body, _ := io.ReadAll(io.LimitReader(response.Body, 32<<10))
					result["status_code"] = response.StatusCode
					result["healthy"] = response.StatusCode >= 200 && response.StatusCode < 400
					var decoded any
					if json.Unmarshal(body, &decoded) == nil {
						result["response"] = decoded
					} else {
						result["response"] = strings.TrimSpace(string(body))
					}
				}
				mu.Lock()
				results[name] = result
				mu.Unlock()
			}()
		}
		wg.Wait()
		return results, []Source{{Service: "cores", Entity: "health"}}, nil, nil
	})

	addTool(server, "cores.data.dictionary", "Describe available Cores data", "Return the supported business entities, important fields, relationships, exclusions and relevant tools. Use this when deciding how to answer a new Cores question.", func(_ context.Context, _ struct{}) (any, []Source, []string, error) {
		return dataDictionary(), []Source{{Service: "cores-mcp", Entity: "data_dictionary"}}, nil, nil
	})
}

func firstRow(rows []map[string]any) any {
	if len(rows) == 0 {
		return map[string]any{}
	}
	return rows[0]
}

func dataDictionary() map[string]any {
	return map[string]any{
		"rental":      map[string]any{"entities": []string{"jobs", "customers (limited)", "venues", "requirements", "packages", "external rentals", "staffing"}, "time_basis": "job start/end dates"},
		"warehouse":   map[string]any{"entities": []string{"products", "devices", "locations", "cases", "relations", "defects", "maintenance", "inventory counts", "movements", "cables"}, "availability": "quantity stock for bulk items; available device count for serialized items"},
		"planner":     map[string]any{"entities": []string{"plans", "tasks", "buckets", "assignees", "goals", "sprints", "dependencies"}},
		"procurement": map[string]any{"entities": []string{"products", "offers", "suppliers", "requisitions", "orders", "receipts", "price history", "warehouse links"}, "money": "integer cents unless a field explicitly says otherwise"},
		"excluded":    []string{"password hashes", "session and API tokens", "2FA secrets", "bank details", "document bodies", "employee private addresses", "unnecessary customer contact details", "arbitrary SQL"},
	}
}

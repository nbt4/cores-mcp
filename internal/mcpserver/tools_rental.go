package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/store"
)

func registerRentalTools(server *mcp.Server, db *store.Store) {
	rowsTool(server, db, "rental.jobs.search", "Search rental jobs", "Search jobs by code, description, customer, venue, or status. Returns schedule and commercial totals without customer contact data.", "rentalcore", "job", func(input SearchInput) (string, []any) {
		return `SELECT j.jobid AS job_id, j.job_code, j.description, s.status, j.startdate AS start_date, j.enddate AS end_date,
                       COALESCE(NULLIF(c.companyname,''), NULLIF(c.name,''), TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS customer,
                       v.name AS venue, j.revenue, j.final_revenue, j.updated_at
                  FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid LEFT JOIN customers c ON c.customerid=j.customerid
                  LEFT JOIN venues v ON v.id=j.venue_id
                 WHERE j.deleted_at IS NULL AND ($1='' OR j.job_code ILIKE $2 OR j.description ILIKE $2 OR s.status ILIKE $2
                    OR c.companyname ILIKE $2 OR c.name ILIKE $2 OR v.name ILIKE $2)
                 ORDER BY j.startdate DESC NULLS LAST, j.jobid DESC LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})

	addTool(server, "rental.jobs.get", "Get rental job context", "Get a job and its product, device, package, rental-equipment, venue, and staffing context by numeric ID or job code.", func(ctx context.Context, input IDInput) (any, []Source, []string, error) {
		jobRows, err := db.Query(ctx, `SELECT j.jobid AS job_id,j.job_code,j.description,s.status,j.startdate AS start_date,j.enddate AS end_date,
                   COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS customer,
                   v.name AS venue,v.city AS venue_city,j.revenue,j.final_revenue,j.discount,j.discount_type,j.updated_at
              FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid LEFT JOIN customers c ON c.customerid=j.customerid
              LEFT JOIN venues v ON v.id=j.venue_id WHERE j.deleted_at IS NULL AND (j.jobid::text=$1 OR j.job_code=$1) LIMIT 1`, input.ID)
		if err != nil || len(jobRows) == 0 {
			return jobRows, []Source{{Service: "rentalcore", Entity: "job", ID: input.ID}}, nil, err
		}
		jobID := jobRows[0]["job_id"]
		requirements, err := db.Query(ctx, `SELECT p.productid AS product_id,p.name,p.product_code,p.tracking_mode,r.quantity,
                   count(DISTINCT jd.deviceid) FILTER (WHERE d.productid=p.productid) AS assigned_devices
              FROM job_product_requirements r JOIN products p ON p.productid=r.product_id
              LEFT JOIN job_devices jd ON jd.jobid=r.job_id LEFT JOIN devices d ON d.deviceid=jd.deviceid
             WHERE r.job_id=$1 GROUP BY p.productid,r.quantity ORDER BY p.name`, jobID)
		if err != nil {
			return nil, nil, nil, err
		}
		packages, err := db.Query(ctx, `SELECT jp.job_package_id,pp.id AS package_id,pp.name,jp.quantity,jp.custom_price,jp.notes
              FROM job_packages jp JOIN product_packages pp ON pp.id=jp.package_id WHERE jp.job_id=$1 ORDER BY pp.name`, jobID)
		if err != nil {
			return nil, nil, nil, err
		}
		rental, err := db.Query(ctx, `SELECT re.id,re.name,re.supplier,re.category,jre.quantity,jre.days_used,jre.total_cost
              FROM job_rental_equipment jre JOIN rental_equipment re ON re.id=jre.equipment_id WHERE jre.job_id=$1 ORDER BY re.name`, jobID)
		if err != nil {
			return nil, nil, nil, err
		}
		staff, err := db.Query(ctx, `SELECT e.id,TRIM(CONCAT_WS(' ',e.first_name,e.last_name)) AS employee,je.role
              FROM job_employees je JOIN employees e ON e.id=je.employee_id WHERE je.job_id=$1 ORDER BY employee`, jobID)
		if err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"job": jobRows[0], "product_requirements": requirements, "packages": packages, "external_rentals": rental, "staffing": staff},
			[]Source{{Service: "rentalcore", Entity: "job", ID: fmt.Sprint(jobID)}}, nil, nil
	})

	windowRowsTool(server, db, "rental.jobs.upcoming", "List upcoming rental jobs", "List jobs overlapping a date window with requirement, device, and package counts.", "rentalcore", "job", func(input WindowInput, from, to time.Time) (string, []any) {
		return `SELECT j.jobid AS job_id,j.job_code,j.description,s.status,j.startdate AS start_date,j.enddate AS end_date,
                       COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS customer,
                       count(DISTINCT r.id) AS requirement_lines,count(DISTINCT jd.deviceid) AS assigned_devices,count(DISTINCT jp.job_package_id) AS packages
                  FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid LEFT JOIN customers c ON c.customerid=j.customerid
                  LEFT JOIN job_product_requirements r ON r.job_id=j.jobid LEFT JOIN job_devices jd ON jd.jobid=j.jobid LEFT JOIN job_packages jp ON jp.job_id=j.jobid
                 WHERE j.deleted_at IS NULL AND j.startdate <= $2 AND COALESCE(j.enddate,j.startdate) >= $1
                 GROUP BY j.jobid,s.status,c.companyname,c.name,c.firstname,c.lastname ORDER BY j.startdate,j.jobid LIMIT $3`, []any{from, to, db.Limit(input.Limit)}
	})

	windowRowsTool(server, db, "rental.requirements.list", "List job material requirements", "Return product quantities required by jobs in a date window, grouped by job and product.", "rentalcore", "job_product_requirement", func(input WindowInput, from, to time.Time) (string, []any) {
		return `SELECT j.jobid AS job_id,j.job_code,j.description,j.startdate AS start_date,j.enddate AS end_date,
                       p.productid AS product_id,p.name AS product,p.product_code,p.tracking_mode,r.quantity
                  FROM job_product_requirements r JOIN jobs j ON j.jobid=r.job_id JOIN products p ON p.productid=r.product_id
                 WHERE j.deleted_at IS NULL AND j.startdate <= $2 AND COALESCE(j.enddate,j.startdate) >= $1
                 ORDER BY j.startdate,p.name LIMIT $3`, []any{from, to, db.Limit(input.Limit)}
	})

	rowsTool(server, db, "rental.customers.search", "Search customers", "Search active customer organizations and names. Returns only identity, type, city, country, and activity counts; contact details and notes are excluded.", "rentalcore", "customer", func(input SearchInput) (string, []any) {
		return `SELECT c.customerid AS customer_id,COALESCE(NULLIF(c.companyname,''),NULLIF(c.name,''),TRIM(CONCAT_WS(' ',c.firstname,c.lastname))) AS name,
                       c.customertype,c.city,c.country,c.is_customer,c.is_supplier,count(j.jobid) AS jobs,max(j.startdate) AS latest_job
                  FROM customers c LEFT JOIN jobs j ON j.customerid=c.customerid AND j.deleted_at IS NULL
                 WHERE COALESCE(c.is_archived,false)=false AND ($1='' OR c.companyname ILIKE $2 OR c.name ILIKE $2 OR c.firstname ILIKE $2 OR c.lastname ILIKE $2 OR c.city ILIKE $2)
                 GROUP BY c.customerid ORDER BY latest_job DESC NULLS LAST,name LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})

	rowsTool(server, db, "rental.venues.search", "Search venues", "Search event venues and return location plus usage counts without private contact details or notes.", "rentalcore", "venue", func(input SearchInput) (string, []any) {
		return `SELECT v.id AS venue_id,v.name,v.city,v.zip,count(j.jobid) AS jobs,max(j.startdate) AS latest_job
                  FROM venues v LEFT JOIN jobs j ON j.venue_id=v.id AND j.deleted_at IS NULL
                 WHERE $1='' OR v.name ILIKE $2 OR v.city ILIKE $2 OR v.zip ILIKE $2
                 GROUP BY v.id ORDER BY jobs DESC,v.name LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})

	summaryRowsTool(server, db, "rental.revenue.summary", "Summarize rental revenue", "Aggregate planned and final rental revenue by month and status for a date window.", "rentalcore", "job", func(input SummaryInput, from, to time.Time) (string, []any) {
		return `SELECT date_trunc('month',j.startdate)::date AS month,s.status,count(*) AS jobs,
                       round(sum(COALESCE(j.revenue,0)),2) AS planned_revenue,round(sum(COALESCE(j.final_revenue,j.revenue,0)),2) AS final_revenue
                  FROM jobs j LEFT JOIN status s ON s.statusid=j.statusid
                 WHERE j.deleted_at IS NULL AND j.startdate BETWEEN $1 AND $2 GROUP BY 1,2 ORDER BY 1,2`, []any{from, to}
	})

	windowRowsTool(server, db, "rental.staffing.requirements", "Get job staffing", "List employee assignments and skill coverage for jobs in a date window. Private employee contact and financial data are excluded.", "rentalcore", "job_employee", func(input WindowInput, from, to time.Time) (string, []any) {
		return `SELECT j.jobid AS job_id,j.job_code,j.description,j.startdate AS start_date,j.enddate AS end_date,
                       e.id AS employee_id,TRIM(CONCAT_WS(' ',e.first_name,e.last_name)) AS employee,je.role,
                       COALESCE(string_agg(DISTINCT sk.name,', '),'') AS skills
                  FROM jobs j LEFT JOIN job_employees je ON je.job_id=j.jobid LEFT JOIN employees e ON e.id=je.employee_id
                  LEFT JOIN employee_skills es ON es.employee_id=e.id LEFT JOIN skills sk ON sk.id=es.skill_id
                 WHERE j.deleted_at IS NULL AND j.startdate <= $2 AND COALESCE(j.enddate,j.startdate) >= $1
                 GROUP BY j.jobid,e.id,je.role ORDER BY j.startdate,j.jobid,employee LIMIT $3`, []any{from, to, db.Limit(input.Limit)}
	})

	rowsTool(server, db, "rental.external_equipment.list", "List external rental equipment", "List active external-rental catalog items and their historic job usage and cost.", "rentalcore", "rental_equipment", func(input SearchInput) (string, []any) {
		return `SELECT re.id,re.name,re.supplier,re.category,re.description,re.rental_price,re.customer_price,
                       count(jre.job_id) AS job_uses,round(COALESCE(sum(jre.total_cost),0),2) AS historic_cost
                  FROM rental_equipment re LEFT JOIN job_rental_equipment jre ON jre.equipment_id=re.id
                 WHERE re.is_active=true AND ($1='' OR re.name ILIKE $2 OR re.supplier ILIKE $2 OR re.category ILIKE $2)
                 GROUP BY re.id ORDER BY job_uses DESC,re.name LIMIT $3 OFFSET $4`, []any{input.Query, searchPattern(input.Query), db.Limit(input.Limit), cleanOffset(input.Offset)}
	})
}

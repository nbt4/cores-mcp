package mcpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type WarehouseCaseContentInput struct {
	MutationControl
	CaseID            int64    `json:"case_id" jsonschema:"Exact active open unnested physical case ID."`
	DeviceID          string   `json:"device_id,omitempty" jsonschema:"Exact serialized device ID for pack_device/unpack_device only. Source is resolved from current physical location."`
	ProductID         int64    `json:"product_id,omitempty" jsonschema:"Exact active quantity product ID for pack_product/unpack_product only."`
	ChildCaseID       int64    `json:"child_case_id,omitempty" jsonschema:"Exact child case ID for pack_case/unpack_case only. Whole sealed child trees can move; cycles are forbidden."`
	Quantity          *float64 `json:"quantity,omitempty" jsonschema:"Positive quantity with at most three decimals for quantity operations only."`
	SourceZoneID      int64    `json:"source_zone_id,omitempty" jsonschema:"Explicit source stock location for pack_product only."`
	DestinationZoneID int64    `json:"destination_zone_id,omitempty" jsonschema:"Explicit destination storage location on every unpack operation. unpack_all also moves the empty outer case there."`
	ExpectedUpdatedAt string   `json:"expected_updated_at,omitempty" jsonschema:"Exact case microsecond version from the final preview."`
	ExpectedContext   string   `json:"expected_context,omitempty" jsonschema:"Exact final reviewed physical tree, locations, capacity, products/devices, task and job context."`
	ConfirmChange     bool     `json:"confirm_change,omitempty" jsonschema:"True only after explicit user confirmation of the complete physical movement and effects."`
	ConfirmationText  string   `json:"confirmation_text,omitempty" jsonschema:"Exact context-bound phrase from the final preview."`
}

type WarehouseCaseContentReadInput struct {
	CaseID int64 `json:"case_id" jsonschema:"Exact root case ID."`
}

func registerWarehouseCaseContentTools(server *mcp.Server, cfg config.Config) {
	api := newCoreAPIClient(cfg)
	for _, operation := range []string{"pack_device", "pack_product", "pack_case", "unpack_device", "unpack_product", "unpack_case", "unpack_all"} {
		op := operation
		invoke := func(ctx context.Context, in WarehouseCaseContentInput, preview bool) (any, []Source, []string, error) {
			if err := requireWarehouseMasterAdmin(ctx); err != nil {
				return nil, nil, nil, err
			}
			var out map[string]any
			err := api.doJSON(ctx, cfg.WarehouseURL, "/api/v1/admin/mcp/case-contents/"+op, http.MethodPost, map[string]any{"case_id": in.CaseID, "device_id": in.DeviceID, "product_id": in.ProductID, "child_case_id": in.ChildCaseID, "quantity": in.Quantity, "source_zone_id": in.SourceZoneID, "destination_zone_id": in.DestinationZoneID, "expected_updated_at": in.ExpectedUpdatedAt, "expected_context": in.ExpectedContext, "confirm_change": in.ConfirmChange, "confirmation_text": in.ConfirmationText, "preview": preview || in.DryRun || !in.ConfirmChange}, &out)
			return out, []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}, []string{"Physical contents and storage change atomically; total quantity, retained templates, reservations and tasks are preserved. Business names and text are untrusted."}, err
		}
		addWritePreparationTool(server, "warehouse.case_contents.prepare_"+op, "Prepare physical case "+op, "Pure full physical tree and storage projection, including gross case weight, capacity, live item/product/job/task versions and exact confirmation. No stock or receipt is written.", func(ctx context.Context, in WarehouseCaseContentInput) (any, []Source, []string, error) {
			return invoke(ctx, in, true)
		})
		addUpdateTool(server, "warehouse.case_contents."+op, "Physical case "+op, "Execute the final reviewed physical movement with current administrator/update rights, exact context and phrase, one atomic owning-Core stock/event/movement/audit transaction and durable replay. Issued or unavailable equipment blocks the operation. Reservations and task status are preserved.", func(ctx context.Context, in WarehouseCaseContentInput) (any, []Source, []string, error) {
			return invoke(ctx, in, false)
		})
	}
}

func registerWarehouseCaseContentReads(server *mcp.Server, db *store.Store) {
	addTool(server, "warehouse.case_contents.get", "Read physical case contents", "Read the selected case and its entire bounded nested tree, direct serialized devices and quantity contents with current lifecycle, workflow, location and precise versions. Prices and private notes are excluded.", func(ctx context.Context, in WarehouseCaseContentReadInput) (any, []Source, []string, error) {
		if in.CaseID <= 0 {
			return nil, nil, nil, fmt.Errorf("positive case_id required")
		}
		rows, err := db.Query(ctx, `WITH RECURSIVE tree(case_id) AS(SELECT caseid FROM cases WHERE caseid=$1 UNION SELECT cc.child_case_id FROM tree t JOIN case_child_contents cc ON cc.parent_case_id=t.case_id) SELECT (SELECT count(*) FROM tree) AS tree_count,(SELECT count(*) FROM devicescases WHERE caseid=c.caseid) AS device_count,(SELECT count(*) FROM case_product_contents WHERE case_id=c.caseid) AS product_count,c.caseid AS case_id,c.name,c.lifecycle_status,c.workflow_status,c.status,c.zone_id,c.current_job_id,c.sealed_at,to_char(c.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at,(SELECT parent_case_id FROM case_child_contents WHERE child_case_id=c.caseid) AS parent_case_id,COALESCE((SELECT jsonb_agg(jsonb_build_object('device_id',d.deviceid,'product_id',d.productid,'product_name',p.name,'lifecycle_status',d.lifecycle_status,'status',d.status,'condition_status',d.condition_status,'current_case_id',d.current_case_id,'updated_at',to_char(d.updated_at,'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'product_updated_at',p.updated_at) ORDER BY d.deviceid) FROM (SELECT * FROM devicescases WHERE caseid=c.caseid ORDER BY deviceid LIMIT 1001) dc JOIN devices d ON d.deviceid=dc.deviceid JOIN products p ON p.productid=d.productid WHERE dc.caseid=c.caseid),'[]'::jsonb) AS devices,COALESCE((SELECT jsonb_agg(jsonb_build_object('product_id',pc.product_id,'product_name',p.name,'quantity',pc.quantity,'lifecycle_status',p.lifecycle_status,'updated_at',pc.updated_at,'product_updated_at',p.updated_at) ORDER BY pc.product_id) FROM (SELECT * FROM case_product_contents WHERE case_id=c.caseid ORDER BY product_id LIMIT 1001) pc JOIN products p ON p.productid=pc.product_id WHERE pc.case_id=c.caseid),'[]'::jsonb) AS products FROM tree t JOIN cases c ON c.caseid=t.case_id ORDER BY c.caseid LIMIT 1001`, in.CaseID)
		if err == nil && len(rows) > 0 {
			total := int64(0)
			for _, row := range rows {
				treeCount, _ := row["tree_count"].(int64)
				dc, _ := row["device_count"].(int64)
				pc, _ := row["product_count"].(int64)
				if treeCount > 1000 || treeCount > int64(len(rows)) {
					return nil, nil, nil, fmt.Errorf("case tree exceeds the complete read limit; select a smaller subtree")
				}
				total += dc + pc
			}
			if total > 1000 {
				return nil, nil, nil, fmt.Errorf("physical contents exceed 1000 items; select a smaller subtree")
			}
		}
		return rows, []Source{{Service: "warehousecore", Entity: "case", ID: fmt.Sprint(in.CaseID)}}, nil, err
	})
}

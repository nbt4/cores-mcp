package mcpserver

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/store"
)

func TestWarehouseStandaloneMasterPreparations(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_WAREHOUSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_WAREHOUSE_TEST_DATABASE_URL to a disposable _test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("integration test requires a dedicated _test database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_warehouse_master_create_test"
	if _, err := database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer database.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	if _, err := database.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE manufacturer(manufacturerid SERIAL PRIMARY KEY,name TEXT,website TEXT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE brands(brandid SERIAL PRIMARY KEY,name TEXT,manufacturerid INT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE products(productid SERIAL PRIMARY KEY,name TEXT,manufacturerid INT,brandid INT,categoryid INT,subcategoryid TEXT,subbiercategoryid TEXT)`,
		`CREATE TABLE categories(categoryid SERIAL PRIMARY KEY,name TEXT,abbreviation TEXT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE subcategories(subcategoryid TEXT PRIMARY KEY,name TEXT,abbreviation TEXT,categoryid INT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE subbiercategories(subbiercategoryid TEXT PRIMARY KEY,name TEXT,abbreviation TEXT,subcategoryid TEXT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE storage_zones(zone_id SERIAL PRIMARY KEY,code TEXT,barcode TEXT,name TEXT,location TEXT,process_role TEXT,operational_status TEXT,is_active BOOLEAN,parent_zone_id INT,type TEXT DEFAULT 'other',location_kind TEXT DEFAULT 'area',description TEXT,capacity INT,capacity_mode TEXT DEFAULT 'item_count',is_storable BOOLEAN DEFAULT TRUE,pick_sequence INT,max_weight_kg NUMERIC,max_volume_m3 NUMERIC,inventory_frequency_days INT,updated_at TIMESTAMP DEFAULT '2026-09-30 10:15:00.123456')`,
		`CREATE TABLE devices(deviceid TEXT,zone_id INT,lifecycle_status TEXT,status TEXT)`,
		`CREATE TABLE cases(caseid INT,zone_id INT)`,
		`CREATE TABLE product_locations(product_id INT,zone_id INT,quantity NUMERIC)`,
		`INSERT INTO manufacturer(name,website) VALUES('MA Lighting','https://www.malighting.com'),('Robe Lighting',NULL)`,
		`INSERT INTO brands(name,manufacturerid) VALUES('grandMA3',1)`,
		`INSERT INTO categories(name,abbreviation) VALUES('Lighting','LT')`,
		`INSERT INTO subcategories VALUES('sub-1','Control','CTRL',1)`,
		`INSERT INTO subbiercategories VALUES('third-1','Network','NET','sub-1')`,
		`INSERT INTO storage_zones(code,barcode,name,location,process_role,operational_status,is_active) VALUES('MAIN','LOC-MAIN','Main Warehouse','Berlin','storage','available',true)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db := store.New(database, 5*time.Second, 200)
	run := func(fn func(context.Context) (preparedMutation, error)) (preparedMutation, error) {
		var p preparedMutation
		var resultErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "11", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": true}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			p, resultErr = fn(r.Context())
		}))
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), r)
		return p, resultErr
	}
	manufacturer := WarehouseManufacturerCreateInput{Name: "ma lighting", Website: "www.malighting.com"}
	p, err := run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseManufacturerCreate(ctx, db, manufacturer)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_manufacturer") || *p.Draft["website"].(*string) != "https://www.malighting.com" {
		t.Fatalf("manufacturer duplicate: %#v %v", p, err)
	}
	manufacturer.Name = "New Manufacturer"
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseManufacturerCreate(ctx, db, manufacturer)
	})
	if err != nil || !p.Ready {
		t.Fatalf("standalone manufacturer draft: %#v %v", p, err)
	}
	brand := WarehouseBrandCreateInput{Name: "grandMA3", ManufacturerID: 1}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseBrandCreate(ctx, db, brand)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_brand") {
		t.Fatalf("brand duplicate: %#v %v", p, err)
	}
	brand.Name = "New Product Line"
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseBrandCreate(ctx, db, brand)
	})
	if err != nil || !p.Ready || p.Draft["manufacturer_id"] != int64(1) {
		t.Fatalf("standalone brand draft: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "category", "lighting", "LT", nil, false)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("category duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "subcategory", "Control", "", int64(1), false)
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("subcategory duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseCategoryCreate(ctx, db, "third_category", "Data", "", "sub-1", false)
	})
	if err != nil || !p.Ready || p.Draft["subcategory_id"] != "sub-1" {
		t.Fatalf("third category draft: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseLocationCreate(ctx, db, WarehouseLocationCreateInput{Code: "main", Name: "Main Warehouse"})
	})
	if err != nil || p.Ready || !containsString(p.Missing, "duplicate_location") {
		t.Fatalf("location duplicate: %#v %v", p, err)
	}
	p, err = run(func(ctx context.Context) (preparedMutation, error) {
		return prepareWarehouseLocationCreate(ctx, db, WarehouseLocationCreateInput{Code: "SHELF-A", Name: "Shelf A", ParentZoneID: 1})
	})
	if err != nil || !p.Ready || p.Draft["parent_zone_id"] != int64(1) {
		t.Fatalf("location draft: %#v %v", p, err)
	}
	name := "Main Warehouse New"
	update := WarehouseLocationUpdateInput{ZoneID: 1, Name: &name, AllowSimilar: true}
	preview := func(input WarehouseLocationUpdateInput) preparedMutation {
		p, err := run(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseLocationUpdate(ctx, db, input)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p = preview(update)
	if !p.Ready || len(p.Diff) != 1 || p.Draft["expected_updated_at"] != "2026-09-30T10:15:00.123456Z" || len(p.Draft) != 17 {
		t.Fatalf("location update preview: %#v", p)
	}
	update.ConfirmUpdate = true
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing update version: %#v", p)
	}
	update.ExpectedUpdatedAt = "2026-09-30T10:15:00Z"
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("stale update version: %#v", p)
	}
	update.ExpectedUpdatedAt = "2026-09-30T10:15:00.123456Z"
	p = preview(update)
	if !p.Ready {
		t.Fatalf("exact update version: %#v", p)
	}
	self := int64(1)
	update.ParentZoneID = &self
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "parent_zone_id") {
		t.Fatalf("self cycle: %#v", p)
	}
	update.ParentZoneID = nil
	if _, err := database.Exec(`INSERT INTO storage_zones(code,barcode,name,process_role,operational_status,is_active,parent_zone_id) VALUES('CHILD','LOC-CHILD','Child Shelf','storage','available',true,1),('OTHER','LOC-OTHER','Another Site','storage','available',true,NULL)`); err != nil {
		t.Fatal(err)
	}
	childID := int64(2)
	update.ParentZoneID = &childID
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "parent_zone_id") {
		t.Fatalf("child cycle: %#v", p)
	}
	update.ParentZoneID = nil
	duplicate := "other"
	update.Code = &duplicate
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "duplicate_location") {
		t.Fatalf("duplicate code: %#v", p)
	}
	update.Code = nil
	if _, err := database.Exec(`INSERT INTO devices VALUES('DEV-1',1,'active','in_storage'),('DEV-2',1,'active','checked_out'),('DEV-3',1,'archived','in_storage'); INSERT INTO product_locations VALUES(1,1,2)`); err != nil {
		t.Fatal(err)
	}
	capacity := float64(2)
	update.Capacity = &capacity
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "capacity") {
		t.Fatalf("occupied capacity: %#v", p)
	}
	capacity = 3.5
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "capacity") {
		t.Fatalf("fraction capacity: %#v", p)
	}
	capacity = 3
	p = preview(update)
	if !p.Ready || p.Draft["capacity"] != float64(3) {
		t.Fatalf("numeric capacity: %#v", p)
	}
	storable := false
	update.IsStorable = &storable
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "is_storable") {
		t.Fatalf("occupied nonstorable: %#v", p)
	}
	update.IsStorable = nil
	update.ClearFields = []string{"capacity"}
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "clear_fields") {
		t.Fatalf("set and clear accepted: %#v", p)
	}
	update.Capacity = nil
	p = preview(update)
	if !p.Ready || p.Draft["capacity"] != nil {
		t.Fatalf("clear capacity: %#v", p)
	}
	update.ClearFields = []string{"is_active"}
	p = preview(update)
	if p.Ready || !containsString(p.Missing, "clear_fields") {
		t.Fatalf("archive through clear: %#v", p)
	}
	if _, err := prepareWarehouseLocationUpdate(context.Background(), nil, update); err == nil {
		t.Fatal("unauthenticated location detail read accepted")
	}
	if got := requiredMutationScope("warehouse.locations.update"); got != "cores:warehouse:update" {
		t.Fatalf("location update scope: %s", got)
	}

	masterPreview := func(entity string, id int64, name, website *string, parent *int64, clear bool, expected string, confirmed bool) preparedMutation {
		p, err := run(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseMasterUpdate(ctx, db, entity, id, name, website, parent, clear, true, expected, confirmed)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	newName := "New Name"
	host := "new.example"
	exact := "2026-09-30T10:15:00.123456Z"
	p = masterPreview("manufacturer", 1, &newName, &host, nil, false, "", false)
	if !p.Ready || len(p.Diff) != 2 || p.Draft["website"] != "https://new.example" || p.Draft["expected_updated_at"] != exact {
		t.Fatalf("manufacturer update %#v", p)
	}
	p = masterPreview("manufacturer", 1, &newName, nil, nil, false, "", true)
	if p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("missing master version %#v", p)
	}
	p = masterPreview("manufacturer", 1, &newName, nil, nil, false, "old", true)
	if p.Ready || !containsString(p.Missing, "expected_updated_at") {
		t.Fatalf("stale master version %#v", p)
	}
	empty := ""
	p = masterPreview("manufacturer", 1, nil, &empty, nil, false, exact, true)
	if !p.Ready || p.Draft["website"] != nil || len(p.Diff) != 1 {
		t.Fatalf("clear website %#v", p)
	}
	invalid := "ftp://example.com"
	p = masterPreview("manufacturer", 1, nil, &invalid, nil, false, "", false)
	if p.Ready || !containsString(p.Missing, "website") {
		t.Fatalf("invalid website %#v", p)
	}
	duplicateName := "robe lighting"
	p = masterPreview("manufacturer", 1, &duplicateName, nil, nil, false, "", false)
	if p.Ready || !containsString(p.Missing, "duplicate_manufacturer") {
		t.Fatalf("duplicate manufacturer %#v", p)
	}
	p = masterPreview("manufacturer", 1, nil, nil, nil, false, "", false)
	if p.Ready || !containsString(p.Missing, "changed_fields") {
		t.Fatalf("manufacturer no-op %#v", p)
	}
	parent := int64(2)
	p = masterPreview("brand", 1, &newName, nil, &parent, false, "", false)
	if !p.Ready || len(p.Diff) != 2 || p.Draft["manufacturer_id"] != int64(2) {
		t.Fatalf("brand reassociation %#v", p)
	}
	if _, err := database.Exec(`INSERT INTO products(name,manufacturerid,brandid) VALUES('Console',1,1)`); err != nil {
		t.Fatal(err)
	}
	p = masterPreview("brand", 1, nil, nil, &parent, false, "", false)
	if p.Ready || !containsString(p.Missing, "linked_products") {
		t.Fatalf("product conflict %#v", p)
	}
	p = masterPreview("brand", 1, &newName, nil, nil, false, "", false)
	if !p.Ready || len(p.Diff) != 1 {
		t.Fatalf("brand rename with products %#v", p)
	}
	p = masterPreview("brand", 1, nil, nil, nil, true, "", false)
	if p.Ready || !containsString(p.Missing, "linked_products") {
		t.Fatalf("clear linked brand manufacturer %#v", p)
	}
	if _, err := database.Exec(`DELETE FROM products;INSERT INTO brands(name,manufacturerid) VALUES('Existing Brand',2)`); err != nil {
		t.Fatal(err)
	}
	duplicateName = "Existing Brand"
	p = masterPreview("brand", 1, &duplicateName, nil, &parent, false, "", false)
	if p.Ready || !containsString(p.Missing, "duplicate_brand") {
		t.Fatalf("duplicate brand %#v", p)
	}
	p = masterPreview("brand", 1, nil, nil, nil, true, "", false)
	if !p.Ready || p.Draft["manufacturer_id"] != nil {
		t.Fatalf("clear unused brand manufacturer %#v", p)
	}
	p = masterPreview("brand", 1, nil, nil, &parent, true, "", false)
	if p.Ready || !containsString(p.Missing, "clear_manufacturer") {
		t.Fatalf("set and clear manufacturer %#v", p)
	}
	parent = 999999
	p = masterPreview("brand", 1, nil, nil, &parent, false, "", false)
	if p.Ready || !containsString(p.Missing, "manufacturer_id") {
		t.Fatalf("missing manufacturer %#v", p)
	}
	parent = 1 << 40
	p = masterPreview("brand", 1, nil, nil, &parent, false, "", false)
	if p.Ready || !containsString(p.Missing, "manufacturer_id") {
		t.Fatalf("out of range manufacturer %#v", p)
	}
	for _, entity := range []string{"manufacturer", "brand"} {
		if _, err := prepareWarehouseMasterUpdate(context.Background(), nil, entity, 1, &newName, nil, nil, false, false, "", false); err == nil {
			t.Fatal("unauthenticated master preview accepted")
		}
		if got := requiredMutationScope("warehouse." + entity + "s.update"); got != "cores:warehouse:update" {
			t.Fatalf("master scope %s", got)
		}
	}

	categoryPreview := func(kind string, id any, name, abbr *string, parent any, expected string, confirm bool, similar bool) preparedMutation {
		p, err := run(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseCategoryUpdate(ctx, db, kind, id, name, abbr, parent, similar, expected, confirm)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	deletionPreview := func(kind string, id any, expected string, confirm bool) preparedMutation {
		p, err := run(func(ctx context.Context) (preparedMutation, error) {
			return prepareWarehouseCategoryDelete(ctx, db, kind, id, expected, confirm)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	categoryName := "Updated Category Fixture"
	for _, target := range []struct {
		kind string
		id   any
	}{{"category", int64(1)}, {"subcategory", "sub-1"}, {"third_category", "third-1"}} {
		p = categoryPreview(target.kind, target.id, &categoryName, nil, nil, "", false, true)
		if !p.Ready || len(p.Diff) != 1 || p.Draft["expected_updated_at"] != exact {
			t.Fatalf("category update %#v", p)
		}
		p = categoryPreview(target.kind, target.id, &categoryName, nil, nil, "", true, true)
		if p.Ready || !containsString(p.Missing, "expected_updated_at") {
			t.Fatalf("missing category version %#v", p)
		}
		p = categoryPreview(target.kind, target.id, &categoryName, nil, nil, "old", true, true)
		if p.Ready || !containsString(p.Missing, "expected_updated_at") {
			t.Fatalf("stale category version %#v", p)
		}
		p = categoryPreview(target.kind, target.id, nil, nil, nil, "", false, true)
		if p.Ready || !containsString(p.Missing, "changed_fields") {
			t.Fatalf("category no-op %#v", p)
		}
	}
	p = categoryPreview("category", int64(1), nil, &empty, nil, "", false, true)
	if p.Ready || !containsString(p.Missing, "abbreviation") {
		t.Fatalf("empty top abbreviation %#v", p)
	}
	p = categoryPreview("subcategory", "sub-1", nil, &empty, nil, "", false, true)
	if !p.Ready || p.Draft["abbreviation"] != "" {
		t.Fatalf("clear sub abbreviation %#v", p)
	}
	p = categoryPreview("third_category", "third-1", nil, &empty, nil, "", false, true)
	if !p.Ready {
		t.Fatalf("clear third abbreviation %#v", p)
	}
	if _, err := database.Exec(`INSERT INTO categories(name,abbreviation) VALUES('Other Parent','OP');INSERT INTO subcategories(subcategoryid,name,categoryid) VALUES('sub-2','Other Sub',2);INSERT INTO subbiercategories(subbiercategoryid,name,subcategoryid) VALUES('third-2','Other Third','sub-2')`); err != nil {
		t.Fatal(err)
	}
	topParent := int64(2)
	subParent := "sub-2"
	p = categoryPreview("subcategory", "sub-1", nil, nil, &topParent, "", false, true)
	if !p.Ready || p.Draft["category_id"] != int64(2) {
		t.Fatalf("sub move %#v", p)
	}
	p = categoryPreview("third_category", "third-1", nil, nil, &subParent, "", false, true)
	if !p.Ready {
		t.Fatalf("third move %#v", p)
	}
	if _, err := database.Exec(`INSERT INTO products(name,categoryid,subcategoryid,subbiercategoryid) VALUES('Hierarchy fixture',1,'sub-1','third-1')`); err != nil {
		t.Fatal(err)
	}
	p = categoryPreview("subcategory", "sub-1", nil, nil, &topParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "linked_products") {
		t.Fatalf("sub product conflict %#v", p)
	}
	p = categoryPreview("third_category", "third-1", nil, nil, &subParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "linked_products") {
		t.Fatalf("third product conflict %#v", p)
	}
	for _, target := range []struct {
		kind string
		id   any
	}{{"category", int64(1)}, {"subcategory", "sub-1"}, {"third_category", "third-1"}} {
		p = deletionPreview(target.kind, target.id, "", false)
		if p.Ready || !containsString(p.Missing, "active_dependencies") {
			t.Fatalf("delete linked category %#v", p)
		}
	}
	duplicateName = "Other Parent"
	p = categoryPreview("category", int64(1), &duplicateName, nil, nil, "", false, true)
	if p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("duplicate top %#v", p)
	}
	duplicateName = "Other Sub"
	p = categoryPreview("subcategory", "sub-1", &duplicateName, nil, &topParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("duplicate sub %#v", p)
	}
	duplicateName = "Other Third"
	p = categoryPreview("third_category", "third-1", &duplicateName, nil, &subParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "duplicate_category") {
		t.Fatalf("duplicate third %#v", p)
	}
	topParent = 1 << 40
	p = categoryPreview("subcategory", "sub-1", nil, nil, &topParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "category_id") {
		t.Fatalf("out of range parent %#v", p)
	}
	topParent = 99999
	p = categoryPreview("subcategory", "sub-1", nil, nil, &topParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "category_id") {
		t.Fatalf("missing top parent %#v", p)
	}
	subParent = "missing"
	p = categoryPreview("third_category", "third-1", nil, nil, &subParent, "", false, true)
	if p.Ready || !containsString(p.Missing, "subcategory_id") {
		t.Fatalf("missing sub parent %#v", p)
	}
	if _, err := database.Exec(`INSERT INTO categories(name,abbreviation) VALUES('Delete Category Fixture','DEL');INSERT INTO subcategories(subcategoryid,name,categoryid) VALUES('delete-sub','Delete Sub Fixture',2);INSERT INTO subbiercategories(subbiercategoryid,name,subcategoryid) VALUES('delete-third','Delete Third Fixture','sub-2')`); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		kind string
		id   any
	}{{"category", int64(3)}, {"subcategory", "delete-sub"}, {"third_category", "delete-third"}} {
		p = deletionPreview(target.kind, target.id, "", false)
		if !p.Ready || p.Draft["confirmation_text_required"] != warehouseCategoryDeletePhrase(target.kind, target.id) || p.Diff["record"]["after"] != nil {
			t.Fatalf("unused deletion %#v", p)
		}
		p = deletionPreview(target.kind, target.id, "", true)
		if p.Ready || !containsString(p.Missing, "expected_updated_at") {
			t.Fatalf("delete without version %#v", p)
		}
		p = deletionPreview(target.kind, target.id, exact, true)
		if !p.Ready {
			t.Fatalf("versioned deletion %#v", p)
		}
	}
	if _, err := database.Exec(`CREATE TABLE custom_category_ref(category_id INT REFERENCES categories(categoryid))`); err != nil {
		t.Fatal(err)
	}
	p = deletionPreview("category", int64(3), "", false)
	if p.Ready || !containsString(p.Missing, "additional_references") {
		t.Fatalf("additional reference guard %#v", p)
	}
	for _, kind := range []string{"category", "subcategory", "third_category"} {
		if _, err := prepareWarehouseCategoryDelete(context.Background(), nil, kind, nil, "", false); err == nil {
			t.Fatal("unauthenticated category deletion preview accepted")
		}
	}

}

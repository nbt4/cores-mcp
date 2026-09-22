package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestSemanticProductHintsExplainPDUCode(t *testing.T) {
	hints := semanticProductHints("PDU 3", "PDU3-YK")
	joined := strings.Join(hints, " ")
	if !strings.Contains(joined, "Stromverteiler") || !strings.Contains(joined, "3") || !strings.Contains(joined, "Inferenz") {
		t.Fatalf("unexpected hints: %#v", hints)
	}
}

func TestMapPreviewParametersLeavesUnknownValuesForQuestions(t *testing.T) {
	target := map[string]any{}
	definitions := []categoryParameter{
		{Key: "outlets", Label: "Steckplätze", Type: "number"},
		{Key: "color", Label: "Farbe", Type: "select", Options: []string{"Schwarz", "Weiß"}},
		{Key: "switch", Label: "Schalter", Type: "boolean"},
	}
	mapPreviewParameters(target, definitions, map[string]string{"Steckplätze": "3 Stück", "Farbe": "schwarz", "Schalter": "Ja"})
	if target["outlets"] != float64(3) || target["color"] != "Schwarz" || target["switch"] != true {
		t.Fatalf("mapped parameters = %#v", target)
	}
	if _, ok := target["missing"]; ok {
		t.Fatal("invented a missing parameter")
	}
}

func TestSuiteTokenRequiresWriteScopeAndCarriesUserIdentity(t *testing.T) {
	secret := strings.Repeat("w", 48)
	api := newCoreAPIClient(config.Config{JWTSecret: secret})
	verifier := func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "42", Scopes: []string{"cores:read", "cores:write"}, Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"username": "noah", "is_admin": true}}, nil
	}
	var raw string
	handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		raw, err = api.suiteToken(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer test")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	claims := &suiteServiceClaims{}
	parsed, err := jwtlib.ParseWithClaims(raw, claims, func(*jwtlib.Token) (any, error) { return []byte(secret), nil }, jwtlib.WithExpirationRequired())
	if err != nil || !parsed.Valid || claims.UserID != 42 || claims.Username != "noah" || !claims.IsAdmin {
		t.Fatalf("invalid delegated suite token: claims=%#v err=%v", claims, err)
	}
}

func TestProductCreateInputJSONUsesGuidedConfirmationFields(t *testing.T) {
	data, err := json.Marshal(ProductCreateInput{SKU: "PDU3", AcceptIncomplete: true, ConfirmCreation: true})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	if !strings.Contains(encoded, `"accept_incomplete":true`) || !strings.Contains(encoded, `"confirm_creation":true`) {
		t.Fatalf("missing guided controls: %s", encoded)
	}
}

func TestOperationalCreateInputsRequireCompleteDrafts(t *testing.T) {
	plan, err := preparePlannerPlanCreate(context.Background(), nil, PlannerPlanCreateInput{})
	if err != nil || plan.Ready || !containsString(plan.Missing, "name") {
		t.Fatalf("plan draft = %#v, err=%v", plan, err)
	}
	task, err := preparePlannerTaskCreate(context.Background(), nil, PlannerTaskCreateInput{})
	if err != nil || task.Ready || !containsString(task.Missing, "plan_id") || !containsString(task.Missing, "title") {
		t.Fatalf("task draft = %#v, err=%v", task, err)
	}
	warehouseTask, err := prepareWarehouseTaskCreate(context.Background(), nil, WarehouseTaskCreateInput{TaskType: "unknown", Priority: 101})
	if err != nil || warehouseTask.Ready || !containsString(warehouseTask.Missing, "task_type") || !containsString(warehouseTask.Missing, "priority") || !containsString(warehouseTask.Missing, "context") {
		t.Fatalf("warehouse task draft = %#v, err=%v", warehouseTask, err)
	}
}

func TestWarehouseTaskInputJSONUsesExplicitConfirmation(t *testing.T) {
	data, err := json.Marshal(WarehouseTaskCreateInput{TaskType: "count", ConfirmCreation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"confirm_creation":true`) {
		t.Fatalf("missing explicit confirmation: %s", data)
	}
}

func TestRequirementCreateNeedsResolvedReferencesAndQuantity(t *testing.T) {
	prepared, err := prepareRequirementCreate(context.Background(), nil, RequirementCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Ready || !containsString(prepared.Missing, "job_id") || !containsString(prepared.Missing, "product_id") || !containsString(prepared.Missing, "quantity") {
		t.Fatalf("requirement draft = %#v", prepared)
	}
	if len(prepared.Questions) != 3 {
		t.Fatalf("questions = %d, want 3", len(prepared.Questions))
	}
	response := prepared.response("draft")
	if _, ok := response["required_missing_fields"]; !ok {
		t.Fatalf("response lacks required_missing_fields: %#v", response)
	}
}

func TestRequirementProductReferenceUsesManufacturerRelation(t *testing.T) {
	query := strings.ToLower(activeProductReferenceQuery)
	if !strings.Contains(query, "left join manufacturer") || !strings.Contains(query, "m.name") {
		t.Fatalf("product reference query does not resolve manufacturer name: %s", activeProductReferenceQuery)
	}
	if strings.Contains(query, "product_code,manufacturer,model_number") {
		t.Fatalf("product reference query reads nonexistent products.manufacturer: %s", activeProductReferenceQuery)
	}
}

func TestWriteToolsExposeSafeAnnotationsAndSchemas(t *testing.T) {
	server := New(config.Config{EnableWrites: true, JWTSecret: strings.Repeat("s", 48)}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	if len(tools) != 91 {
		t.Fatalf("tool count = %d, want 91", len(tools))
	}
	for _, name := range []string{"procurement.products.prepare_create", "rental.jobs.prepare_create", "rental.requirements.prepare_create", "planner.plans.prepare_create", "planner.tasks.prepare_create", "warehouse.tasks.prepare_create", "warehouse.products.prepare_create", "rental.jobs.prepare_assign_device", "rental.jobs.prepare_update", "rental.requirements.prepare_update", "procurement.orders.prepare_create", "warehouse.movements.prepare_create", "warehouse.devices.prepare_update_status", "procurement.requisitions.prepare_decide", "procurement.orders.prepare_receive"} {
		if tools[name] == nil || tools[name].Annotations == nil || !tools[name].Annotations.ReadOnlyHint {
			t.Fatalf("%s is missing read-only preparation annotation", name)
		}
	}
	for _, name := range []string{"procurement.products.create", "rental.jobs.create", "rental.requirements.create", "planner.plans.create", "planner.tasks.create", "warehouse.tasks.create", "warehouse.products.create", "rental.jobs.assign_device", "procurement.orders.create"} {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("%s is not marked as an additive write", name)
		}
		assertMutationControlSchema(t, tool)
	}
	for _, name := range []string{"rental.jobs.update", "rental.requirements.update", "warehouse.movements.create", "warehouse.devices.update_status", "procurement.requisitions.decide", "procurement.orders.receive"} {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Fatalf("%s is not marked as a state-changing write", name)
		}
		assertMutationControlSchema(t, tool)
	}
}

func assertMutationControlSchema(t *testing.T, tool *mcp.Tool) {
	t.Helper()
	if !strings.Contains(tool.Description, "idempotency_key") || !strings.Contains(tool.Description, "dry_run=true") {
		t.Fatalf("%s description lacks mutation controls: %s", tool.Name, tool.Description)
	}
	schema, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal %s schema: %v", tool.Name, err)
	}
	encoded := string(schema)
	if !strings.Contains(encoded, `"dry_run"`) || !strings.Contains(encoded, `"idempotency_key"`) {
		t.Fatalf("%s schema lacks mutation controls: %s", tool.Name, encoded)
	}
}

func TestWarehouseProductCreateRequiresCoreMasterData(t *testing.T) {
	prepared, err := prepareWarehouseProductCreate(context.Background(), nil, WarehouseProductCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Ready || !containsString(prepared.RequiredMissing, "name") || !containsString(prepared.RequiredMissing, "manufacturer") || !containsString(prepared.RequiredMissing, "category") {
		t.Fatalf("unexpected product draft: %#v", prepared)
	}
	if prepared.Draft["product_type"] != "equipment" || prepared.Draft["tracking_mode"] != "individual" || prepared.Draft["product_kind"] != "standard" {
		t.Fatalf("unexpected defaults: %#v", prepared.Draft)
	}
}

func TestWarehouseMasterMatchingHandlesSpellingVariants(t *testing.T) {
	if score := warehouseMatchScore("MA Lighting", "MA Lighting International GmbH", ""); score < 75 {
		t.Fatalf("manufacturer score = %d, want >= 75", score)
	}
	if score := warehouseMatchScore("grand ma", "grandMA", "MA Lighting"); score < 30 {
		t.Fatalf("brand score = %d, want >= 30", score)
	}
}

func TestEntitySchemaExposesWarehouseProductFields(t *testing.T) {
	schema := renderWritableEntitySchema(writableEntitySchemas()["warehouse.products"])
	fields, ok := schema["fields"].([]map[string]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("missing fields: %#v", schema)
	}
	foundCreateManufacturer := false
	for _, field := range fields {
		if field["name"] == "create_manufacturer" && field["type"] == "boolean" {
			foundCreateManufacturer = true
		}
	}
	if !foundCreateManufacturer {
		t.Fatalf("warehouse product schema lacks create_manufacturer: %#v", fields)
	}
}

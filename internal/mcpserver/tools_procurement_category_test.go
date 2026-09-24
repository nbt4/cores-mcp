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

func TestCategorySchemaValidation(t *testing.T) {
	for _, definitions := range [][]categoryParameter{
		{{Key: "same", Label: "One", Type: "text"}, {Key: "SAME", Label: "Two", Type: "number"}},
		{{Key: "choice", Label: "Choice", Type: "select"}},
		{{Key: "invalid", Label: "Invalid", Type: "secret"}},
	} {
		if len(validateCategorySchema(definitions)) == 0 {
			t.Fatalf("invalid schema accepted: %#v", definitions)
		}
	}
	if len(validateCategorySchema([]categoryParameter{{Key: "power", Label: "Power", Type: "number", Unit: "W"}})) != 0 {
		t.Fatal("valid schema rejected")
	}
}

func TestPrepareCategoryCreateAndUpdate(t *testing.T) {
	dsn := os.Getenv("CORES_MCP_SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CORES_MCP_SUPPLIER_TEST_DATABASE_URL to a disposable PostgreSQL database ending in _test")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("category integration test requires a dedicated _test database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	const schema = "mcp_category_prepare_test"
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
	if _, err := database.Exec(`CREATE TABLE proc_categories (id BIGSERIAL PRIMARY KEY, name TEXT, description TEXT, parameter_schema JSONB, updated_at TIMESTAMPTZ)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO proc_categories (name,description,parameter_schema,updated_at) VALUES
		('Stage Lighting','Lights','[{"key":"power","label":"Power","type":"number","unit":"W"}]','2026-09-24T08:15:00.123456Z'),
		('Video','Screens','[]','2026-09-24T08:15:00Z')`); err != nil {
		t.Fatal(err)
	}
	lookup := store.New(database, 5*time.Second, 200)
	create, err := prepareCategoryCreate(context.Background(), lookup, CategoryCreateInput{Name: "Stage Lighting"})
	if err != nil || create.Ready || !containsString(create.Missing, "duplicate_name") || len(create.RelatedRecords) != 1 {
		t.Fatalf("duplicate category: %#v, %v", create, err)
	}
	create, err = prepareCategoryCreate(context.Background(), lookup, CategoryCreateInput{Name: "Stage Lighting Fixtures"})
	if err != nil || create.Ready || !containsString(create.Missing, "similar_category_review") {
		t.Fatalf("similar category: %#v, %v", create, err)
	}
	create, err = prepareCategoryCreate(context.Background(), lookup, CategoryCreateInput{Name: "Stage Lighting Fixtures", AllowSimilar: true})
	if err != nil || !create.Ready {
		t.Fatalf("reviewed category: %#v, %v", create, err)
	}
	name := "Video Walls"
	input := CategoryUpdateInput{CategoryID: 1, Name: &name}
	run := func(admin bool, value CategoryUpdateInput) (preparedMutation, error) {
		var result preparedMutation
		var prepareErr error
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{UserID: "1", Expiration: time.Now().Add(time.Hour), Extra: map[string]any{"is_admin": admin}}, nil
		}
		handler := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			result, prepareErr = prepareCategoryUpdate(request.Context(), lookup, value)
		}))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", "Bearer test")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return result, prepareErr
	}
	if _, err := run(false, input); err == nil {
		t.Fatal("non-admin category detail read accepted")
	}
	update, err := run(true, input)
	if err != nil || update.Ready || !containsString(update.Missing, "similar_category_review") {
		t.Fatalf("similar category update accepted: %#v, %v", update, err)
	}
	input.AllowSimilar = true
	update, err = run(true, input)
	if err != nil || !update.Ready || update.Draft["expectedUpdatedAt"] != "2026-09-24T08:15:00.123456Z" || len(update.Diff) != 1 {
		t.Fatalf("category update preview: %#v, %v", update, err)
	}
	input.ConfirmUpdate = true
	update, err = run(true, input)
	if err != nil || update.Ready || !containsString(update.Missing, "expected_updated_at") {
		t.Fatalf("missing version accepted: %#v, %v", update, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00Z"
	update, err = run(true, input)
	if err != nil || update.Ready || !containsString(update.Missing, "expected_updated_at") {
		t.Fatalf("stale version accepted: %#v, %v", update, err)
	}
	input.ExpectedUpdatedAt = "2026-09-24T08:15:00.123456Z"
	update, err = run(true, input)
	if err != nil || !update.Ready {
		t.Fatalf("matching version rejected: %#v, %v", update, err)
	}
}

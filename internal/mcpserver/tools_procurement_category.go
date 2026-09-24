package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/store"
)

type CategoryCreateInput struct {
	MutationControl
	Name            string              `json:"name,omitempty" jsonschema:"Required unique category name; at most 160 characters."`
	Description     string              `json:"description,omitempty" jsonschema:"Optional category description."`
	ParameterSchema []categoryParameter `json:"parameter_schema,omitempty" jsonschema:"Complete parameter definitions: unique key, label, type text/number/select/boolean, optional unit, and options for select."`
	AllowSimilar    bool                `json:"allow_similar,omitempty" jsonschema:"Set true only after reviewing similar existing categories and confirming a distinct category."`
	ConfirmCreation bool                `json:"confirm_creation,omitempty" jsonschema:"Set true only after showing the final category draft and receiving explicit confirmation."`
}

type CategoryUpdateInput struct {
	MutationControl
	CategoryID        int64                `json:"category_id,omitempty" jsonschema:"Exact ProcurementCore category ID."`
	Name              *string              `json:"name,omitempty" jsonschema:"Optional replacement category name; at most 160 characters."`
	Description       *string              `json:"description,omitempty" jsonschema:"Optional replacement description; empty string clears it."`
	ParameterSchema   *[]categoryParameter `json:"parameter_schema,omitempty" jsonschema:"Optional complete replacement parameter schema; an empty array removes all definitions."`
	AllowSimilar      bool                 `json:"allow_similar,omitempty" jsonschema:"Set true only after reviewing similar existing categories."`
	ExpectedUpdatedAt string               `json:"expected_updated_at,omitempty" jsonschema:"Exact version returned by prepare_update; required for execution."`
	ConfirmUpdate     bool                 `json:"confirm_update,omitempty" jsonschema:"Set true only after showing the complete before/after diff and receiving explicit confirmation."`
}

func registerCategoryTools(server *mcp.Server, cfg config.Config, db *store.Store) {
	api := newCoreAPIClient(cfg)
	addWritePreparationTool(server, "procurement.categories.prepare_create", "Prepare procurement category", "Validate category name and the complete parameter schema, check duplicate and similar categories, and show the final draft without changing data.", func(ctx context.Context, input CategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareCategoryCreate(ctx, db, input)
		return p.response("draft"), categorySources(p.RelatedRecords), untrustedTextWarning(), err
	})
	addCreateTool(server, "procurement.categories.create", "Create procurement category", "Create a category only after prepare_create, duplicate review, complete preview and explicit confirmation. The owning API commits category, audit and idempotency atomically.", func(ctx context.Context, input CategoryCreateInput) (any, []Source, []string, error) {
		p, err := prepareCategoryCreate(ctx, db, input)
		if err != nil {
			return nil, nil, nil, err
		}
		if !p.Ready {
			return p.response("needs_input"), categorySources(p.RelatedRecords), []string{"No data was changed."}, nil
		}
		if !input.ConfirmCreation {
			return p.response("confirmation_required"), categorySources(p.RelatedRecords), []string{"No data was changed. Show the full draft and obtain explicit confirmation."}, nil
		}
		var created map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, "/api/v1/categories", http.MethodPost, p.Draft, &created); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"creation_status": "created", "category": created}, []Source{{Service: "procurementcore", Entity: "category", ID: fmt.Sprint(created["id"])}}, nil, nil
	})
	addWritePreparationTool(server, "procurement.categories.prepare_update", "Prepare procurement category update", "Load a category for a Procurement administrator, validate changes and show the complete diff and exact version. No data is changed.", func(ctx context.Context, input CategoryUpdateInput) (any, []Source, []string, error) {
		p, err := prepareCategoryUpdate(ctx, db, input)
		return p.response("draft"), categoryUpdateSources(input.CategoryID, p.RelatedRecords), untrustedTextWarning(), err
	})
	addUpdateTool(server, "procurement.categories.update", "Update procurement category", "Update a category only after prepare_update, complete diff, unchanged version and explicit confirmation. Parameter definitions are replaced as one complete schema.", func(ctx context.Context, input CategoryUpdateInput) (any, []Source, []string, error) {
		p, err := prepareCategoryUpdate(ctx, db, input)
		if err != nil || !p.Ready {
			return p.response("needs_input"), categoryUpdateSources(input.CategoryID, p.RelatedRecords), []string{"No data was changed."}, err
		}
		if !input.ConfirmUpdate {
			return p.response("confirmation_required"), categoryUpdateSources(input.CategoryID, p.RelatedRecords), []string{"No data was changed. Show the complete diff and obtain explicit confirmation."}, nil
		}
		var updated map[string]any
		if err := api.doJSON(ctx, cfg.ProcurementURL, fmt.Sprintf("/api/v1/categories/%d", input.CategoryID), http.MethodPut, p.Draft, &updated); err != nil {
			return nil, nil, nil, err
		}
		return map[string]any{"operation_status": "updated", "category": updated, "diff": p.Diff}, []Source{{Service: "procurementcore", Entity: "category", ID: fmt.Sprint(input.CategoryID)}}, nil, nil
	})
}

func categorySources(records []map[string]any) []Source {
	result := []Source{{Service: "procurementcore", Entity: "category_draft"}}
	return append(result, sourcesFor("procurementcore", "category", records)...)
}

func categoryUpdateSources(id int64, records []map[string]any) []Source {
	result := []Source{{Service: "procurementcore", Entity: "category", ID: fmt.Sprint(id)}}
	return append(result, sourcesFor("procurementcore", "category", records)...)
}

func validateCategorySchema(definitions []categoryParameter) []string {
	var invalid []string
	if len(definitions) > 100 {
		return []string{"parameter_schema"}
	}
	if encoded, err := json.Marshal(definitions); err != nil || len(encoded) > 65536 {
		return []string{"parameter_schema"}
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		key := strings.TrimSpace(definition.Key)
		label := strings.TrimSpace(definition.Label)
		if key == "" || key != definition.Key || len([]rune(key)) > 80 || label == "" || len([]rune(label)) > 160 || seen[strings.ToLower(key)] {
			invalid = append(invalid, "parameter_schema")
		}
		seen[strings.ToLower(key)] = true
		if !map[string]bool{"text": true, "number": true, "select": true, "boolean": true}[definition.Type] || len([]rune(definition.Unit)) > 60 || len(definition.Options) > 100 || definition.Type == "select" && len(definition.Options) == 0 {
			invalid = append(invalid, "parameter_schema")
		}
		for _, option := range definition.Options {
			if strings.TrimSpace(option) == "" || len([]rune(option)) > 160 {
				invalid = append(invalid, "parameter_schema")
			}
		}
	}
	return unique(invalid)
}

func checkCategoryDuplicates(ctx context.Context, db *store.Store, name string, excludeID int64, allowSimilar bool) ([]map[string]any, []string, error) {
	if name == "" {
		return nil, nil, nil
	}
	first := strings.Fields(name)[0]
	rows, err := db.Query(ctx, `SELECT id,name FROM proc_categories WHERE id<>$1 AND (lower(name)=lower($2) OR position(lower($3) in lower(name))>0)
		ORDER BY CASE WHEN lower(name)=lower($2) THEN 0 ELSE 1 END,name LIMIT 100`, excludeID, name, first)
	if err != nil {
		return nil, nil, err
	}
	var related []map[string]any
	var problems []string
	for _, row := range rows {
		candidate := fmt.Sprint(row["name"])
		if strings.EqualFold(candidate, name) {
			related = append(related, row)
			problems = append(problems, "duplicate_name")
		} else if warehouseMatchScore(name, candidate, "") >= 50 {
			related = append(related, row)
			if !allowSimilar {
				problems = append(problems, "similar_category_review")
			}
		}
	}
	if len(rows) >= 100 {
		problems = append(problems, "category_search_limit")
	}
	return related, unique(problems), nil
}

func prepareCategoryCreate(ctx context.Context, db *store.Store, input CategoryCreateInput) (preparedOperationalCreate, error) {
	name := strings.TrimSpace(input.Name)
	p := preparedOperationalCreate{Draft: map[string]any{"name": name, "description": strings.TrimSpace(input.Description), "parameterSchema": input.ParameterSchema}}
	if p.Draft["parameterSchema"] == nil {
		p.Draft["parameterSchema"] = []categoryParameter{}
	}
	if name == "" || len([]rune(name)) > 160 {
		p.require("name", "Welcher eindeutige Kategoriename mit höchstens 160 Zeichen soll angelegt werden?")
	}
	if len(validateCategorySchema(input.ParameterSchema)) > 0 {
		p.require("parameter_schema", "Welche gültigen Parameter mit eindeutigen Schlüsseln, Beschriftung, Typ und gegebenenfalls Auswahloptionen gelten?")
	}
	if containsString(p.Missing, "name") {
		return p, nil
	}
	related, problems, err := checkCategoryDuplicates(ctx, db, name, 0, input.AllowSimilar)
	if err != nil {
		return p, err
	}
	p.RelatedRecords = related
	for _, problem := range problems {
		p.require(problem, categoryProblemQuestion(problem))
	}
	p.Ready = len(p.Missing) == 0
	return p, nil
}

func prepareCategoryUpdate(ctx context.Context, db *store.Store, input CategoryUpdateInput) (preparedMutation, error) {
	p := preparedMutation{Draft: map[string]any{}}
	if input.CategoryID <= 0 {
		p.require("category_id", "Welche gültige Kategorien-ID soll geändert werden?", nil)
		p.finish()
		return p, nil
	}
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || info.Extra["is_admin"] != true {
		return p, fmt.Errorf("Procurement administrator permission is required to read and update category details")
	}
	rows, err := db.Query(ctx, `SELECT id,name,description,parameter_schema,
		to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS updated_at
		FROM proc_categories WHERE id=$1`, input.CategoryID)
	if err != nil {
		return p, err
	}
	if len(rows) != 1 {
		p.require("category_id", "Die Kategorie wurde nicht gefunden. Welche gültige ID soll verwendet werden?", nil)
		p.finish()
		return p, nil
	}
	p.Current = rows[0]
	version := rfc3339Value(p.Current["updated_at"])
	var parameters []categoryParameter
	encoded, err := json.Marshal(p.Current["parameter_schema"])
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(encoded, &parameters); err != nil {
		return p, err
	}
	if parameters == nil {
		parameters = []categoryParameter{}
	}
	p.Draft = map[string]any{"name": fmt.Sprint(p.Current["name"]), "description": nullableText(p.Current["description"]), "parameterSchema": parameters, "expectedUpdatedAt": version}
	before := cloneMap(p.Draft)
	if input.Name != nil {
		p.Draft["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		p.Draft["description"] = strings.TrimSpace(*input.Description)
	}
	if input.ParameterSchema != nil {
		p.Draft["parameterSchema"] = *input.ParameterSchema
	}
	p.Diff = map[string]map[string]any{}
	for field, old := range before {
		if field != "expectedUpdatedAt" && !reflect.DeepEqual(old, p.Draft[field]) {
			p.Diff[field] = map[string]any{"before": old, "after": p.Draft[field]}
		}
	}
	if len(p.Diff) == 0 {
		p.require("changes", "Welche Kategorienfelder sollen tatsächlich geändert werden?", nil)
	}
	name := fmt.Sprint(p.Draft["name"])
	if name == "" || len([]rune(name)) > 160 {
		p.require("name", "Welcher eindeutige Kategoriename mit höchstens 160 Zeichen soll gespeichert werden?", nil)
	}
	if len(validateCategorySchema(p.Draft["parameterSchema"].([]categoryParameter))) > 0 {
		p.require("parameter_schema", "Welche gültige vollständige Liste von Parameterdefinitionen soll gespeichert werden?", nil)
	}
	if input.ConfirmUpdate && strings.TrimSpace(input.ExpectedUpdatedAt) == "" {
		p.require("expected_updated_at", "Die genaue Version aus der aktuellen Vorschau muss übernommen werden.", version)
	} else if input.ExpectedUpdatedAt != "" && strings.TrimSpace(input.ExpectedUpdatedAt) != version {
		p.require("expected_updated_at", "Die Kategorie wurde seit der Vorschau geändert. Bitte erneut vorbereiten.", version)
	}
	if name != "" && (input.Name != nil || input.ConfirmUpdate) {
		related, problems, queryErr := checkCategoryDuplicates(ctx, db, name, input.CategoryID, input.AllowSimilar)
		if queryErr != nil {
			return p, queryErr
		}
		p.RelatedRecords = related
		for _, problem := range problems {
			p.require(problem, categoryProblemQuestion(problem), related)
		}
	}
	p.finish()
	return p, nil
}

func categoryProblemQuestion(problem string) string {
	switch problem {
	case "duplicate_name":
		return "Dieser Kategoriename existiert bereits. Bitte vorhandene Kategorie verwenden oder einen anderen Namen wählen."
	case "similar_category_review":
		return "Ähnliche Kategorien existieren bereits. Ist dies wirklich eine andere Kategorie?"
	default:
		return "Die Kategoriensuche ist unvollständig. Bitte den Bestand gezielt prüfen."
	}
}

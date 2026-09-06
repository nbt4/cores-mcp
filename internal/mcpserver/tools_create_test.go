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
	if len(tools) != 63 {
		t.Fatalf("tool count = %d, want 63", len(tools))
	}
	for _, name := range []string{"procurement.products.prepare_create", "rental.jobs.prepare_create"} {
		if tools[name] == nil || tools[name].Annotations == nil || !tools[name].Annotations.ReadOnlyHint {
			t.Fatalf("%s is missing read-only preparation annotation", name)
		}
	}
	for _, name := range []string{"procurement.products.create", "rental.jobs.create"} {
		tool := tools[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("%s is not marked as an additive write", name)
		}
	}
}

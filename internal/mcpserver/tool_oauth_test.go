package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestPositionOAuthDiscoveryAndChallengeAcrossHTTP(t *testing.T) {
	var ownerCalls atomic.Int64
	secret := strings.Repeat("scope-upgrade-test-", 4)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownerCalls.Add(1)
		if r.URL.Path != "/api/v1/mcp/job-positions/create" && r.URL.Path != "/api/v1/mcp/jobs/external-equipment-create" {
			t.Errorf("unexpected owner path %s", r.URL.Path)
		}
		cookie, err := r.Cookie("cores_token")
		if err != nil {
			t.Error(err)
			w.WriteHeader(403)
			return
		}
		claims := &suiteServiceClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil })
		if err != nil || !token.Valid || !claims.FinancialScope || claims.MutationScope != "cores:rental:create" || claims.UserID != 7 {
			t.Error("wrong owner delegation", err, claims)
			w.WriteHeader(403)
			return
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["preview"] != true || body["confirm_change"] == true || r.Header.Get("Idempotency-Key") != "" {
			t.Error("authorization triggered mutation", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"preview":true,"ready_to_execute":true,"operation_status":"confirmation_required","draft":{"job_id":1,"product_id":2,"quantity":1,"unit_price":0}}`)
	}))
	defer owner.Close()
	cfg := config.Config{PublicURL: "https://cores.example", EnableWrites: true, RentalURL: owner.URL, JWTSecret: secret}
	server := New(cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tokens := map[string]*auth.TokenInfo{
		"old":              {UserID: "7", Scopes: []string{"cores:read", "cores:write"}, Extra: map[string]any{"is_admin": true}},
		"read":             {UserID: "7", Scopes: []string{"cores:read"}, Extra: map[string]any{"is_admin": true}},
		"create":           {UserID: "7", Scopes: []string{"cores:read", "cores:rental:create", "cores:rental:financial"}, Extra: map[string]any{"is_admin": true}},
		"legacy-financial": {UserID: "7", Scopes: []string{"cores:read", "cores:write", "cores:rental:financial"}, Extra: map[string]any{"is_admin": true}},
		"non-admin":        {UserID: "7", Scopes: []string{"cores:read", "cores:rental:create", "cores:rental:financial"}, Extra: map[string]any{"is_admin": false}},
		"other-financial":  {UserID: "7", Scopes: []string{"cores:read", "cores:rental:create", "cores:warehouse:financial"}, Extra: map[string]any{"is_admin": true}},
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	verify := func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		info := tokens[raw]
		if info == nil {
			return nil, auth.ErrInvalidToken
		}
		copy := *info
		copy.Expiration = time.Now().Add(time.Hour)
		return &copy, nil
	}
	httpServer := httptest.NewServer(auth.RequireBearerToken(verify, nil)(handler))
	defer httpServer.Close()
	connect := func(token string) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "scope-test", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL, HTTPClient: &http.Client{Transport: accessTestTransport{subject: token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	call := func(session *mcp.ClientSession, name string) *mcp.CallToolResult {
		t.Helper()
		args := map[string]any{"job_id": 1, "product_id": 2, "quantity": 1, "unit_price": 0}
		if strings.HasPrefix(name, "rental.job_external_equipment.") {
			args = map[string]any{"job_id": 1, "equipment_id": 2, "quantity": 1, "days_used": 3}
		}
		if name == "rental.job_positions.get" {
			args = map[string]any{"id": "1"}
		}
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertChallenge := func(result *mcp.CallToolResult, wantScope string) {
		t.Helper()
		if !result.IsError {
			t.Fatal("missing scope accepted", result)
		}
		raw, err := json.Marshal(result.Meta["mcp/www_authenticate"])
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		for _, want := range []string{"Bearer", "resource_metadata", cfg.PublicURL + "/.well-known/oauth-protected-resource", "insufficient_scope", "error_description", wantScope} {
			if !strings.Contains(s, want) {
				t.Fatal("missing OAuth challenge field", want, s)
			}
		}
	}
	old := connect("old")
	for _, name := range []string{"rental.job_external_equipment.prepare_create", "rental.job_external_equipment.create", "rental.job_positions.prepare_create", "rental.job_positions.create", "rental.job_positions.prepare_update", "rental.job_positions.update", "rental.job_positions.prepare_archive", "rental.job_positions.archive", "rental.job_positions.get", "rental.job_positions.search"} {
		if strings.HasSuffix(name, "search") {
			result, err := old.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			assertChallenge(result, "cores:rental:financial")
		} else if strings.Contains(name, "update") || strings.Contains(name, "archive") {
			result, err := old.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"position_id": 1}})
			if err != nil {
				t.Fatal(err)
			}
			assertChallenge(result, "cores:rental:financial")
		} else {
			assertChallenge(call(old, name), "cores:rental:financial")
		}
	}
	if ownerCalls.Load() != 0 {
		t.Fatal("denied scope reached owner")
	}
	// Repeating a denied call cannot silently acquire a grant or reuse confirmation.
	denied := call(old, "rental.job_positions.prepare_create")
	assertChallenge(denied, "cores:rental:financial")
	raw, _ := json.Marshal(denied.Meta)
	if !strings.Contains(string(raw), "cores:write") || strings.Contains(string(raw), "cores:warehouse:financial") {
		t.Fatal("lost previous or added unrelated grant", string(raw))
	}
	assertChallenge(call(connect("read"), "rental.job_positions.prepare_create"), "cores:rental:create")
	assertChallenge(call(connect("other-financial"), "rental.job_positions.prepare_create"), "cores:rental:financial")
	for _, token := range []string{"create", "legacy-financial"} {
		result := call(connect(token), "rental.job_positions.prepare_create")
		if result.IsError || result.Meta["mcp/www_authenticate"] != nil {
			t.Fatal("granted user cannot prepare", result)
		}
	}
	if ownerCalls.Load() != 2 {
		t.Fatal("wrong preview count", ownerCalls.Load())
	}
	for _, token := range []string{"create", "legacy-financial"} {
		result := call(connect(token), "rental.job_external_equipment.prepare_create")
		if result.IsError {
			t.Fatal("authorized external rental preparation rejected", result)
		}
	}
	if ownerCalls.Load() != 4 {
		t.Fatal("external rental preview did not reach owner", ownerCalls.Load())
	}
	nonAdmin := call(connect("non-admin"), "rental.job_positions.prepare_create")
	if !nonAdmin.IsError || nonAdmin.Meta["mcp/www_authenticate"] != nil || ownerCalls.Load() != 4 {
		t.Fatal("administrator denial mistaken for missing scope", nonAdmin)
	}
	// Inspect raw JSON because the Go SDK only models the standard tool fields.
	req, err := http.NewRequest("POST", httpServer.URL, bytes.NewBufferString(`{"jsonrpc":"2.0","id":44,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer old")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "data: ") {
				data = []byte(strings.TrimPrefix(line, "data: "))
				break
			}
		}
	}
	var envelope struct {
		Result struct {
			Tools []struct {
				Name            string                     `json:"name"`
				SecuritySchemes []oauthSecurityScheme      `json:"securitySchemes"`
				Meta            map[string]json.RawMessage `json:"_meta"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err, string(data))
	}
	found := 0
	for _, tool := range envelope.Result.Tools {
		want := positionOAuthScopes(tool.Name)
		if len(want) == 0 {
			continue
		}
		found++
		if len(tool.SecuritySchemes) != 1 || tool.SecuritySchemes[0].Type != "oauth2" || strings.Join(tool.SecuritySchemes[0].Scopes, " ") != strings.Join(want, " ") {
			t.Fatal("incorrect tool scope policy", tool.Name, tool.SecuritySchemes)
		}
		canonical, _ := json.Marshal(tool.SecuritySchemes)
		var mirror []oauthSecurityScheme
		if err = json.Unmarshal(tool.Meta["securitySchemes"], &mirror); err != nil {
			t.Fatal(err)
		}
		mirrored, _ := json.Marshal(mirror)
		if !bytes.Equal(canonical, mirrored) {
			t.Fatal("metadata mirror differs", tool.Name)
		}
	}
	if found != 12 {
		t.Fatal("missing position and job-read discovery policies", found, response.StatusCode)
	}
}

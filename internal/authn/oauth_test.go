package authn

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

func TestValidRedirectURI(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"https://claude.ai/api/mcp/auth_callback", true},
		{"http://localhost:3000/callback", true},
		{"http://127.0.0.1:9999/callback", true},
		{"http://example.com/callback", false},
		{"https://example.com/callback#fragment", false},
		{"javascript:alert(1)", false},
	}
	for _, test := range tests {
		if got := validRedirectURI(test.value); got != test.want {
			t.Errorf("validRedirectURI(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestOAuthAuthorizationCodeFlow(t *testing.T) {
	secret := strings.Repeat("s", 48)
	server, err := NewOAuthServer("https://mcp.example.com", "https://cores.example.com", secret, t.TempDir()+"/clients.json", func(_ context.Context, id uint) (bool, error) { return id == 7, nil })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.Register(mux)

	registration := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"client_name":"Test","redirect_uris":["https://client.example.com/callback"],"token_endpoint_auth_method":"none"}`))
	registration.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	mux.ServeHTTP(registered, registration)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registered.Code, registered.Body.String())
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}

	verifier := "test-code-verifier-with-sufficient-entropy"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	query := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example.com/callback"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {readScope}, "state": {"state-1"}}

	suiteToken, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, suiteClaims{UserID: 7, Username: "tester", RegisteredClaims: jwtlib.RegisteredClaims{ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour))}}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	authorize := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	authorize.AddCookie(&http.Cookie{Name: "cores_token", Value: suiteToken})
	consent := httptest.NewRecorder()
	mux.ServeHTTP(consent, authorize)
	if consent.Code != http.StatusOK || len(consent.Result().Cookies()) == 0 {
		t.Fatalf("authorize status=%d body=%s", consent.Code, consent.Body.String())
	}
	if policy := consent.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "form-action 'self' https://mcp.example.com https://client.example.com;") {
		t.Fatalf("consent CSP does not allow the validated callback origin: %s", policy)
	}
	assertAuthorizationFields(t, consent.Body.String(), query)
	csrf := consent.Result().Cookies()[0]

	form := url.Values{}
	for key, values := range query {
		form[key] = append([]string(nil), values...)
	}
	form.Set("csrf", csrf.Value)
	form.Set("decision", "allow")
	approve := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	approve.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approve.AddCookie(&http.Cookie{Name: "cores_token", Value: suiteToken})
	approve.AddCookie(csrf)
	approved := httptest.NewRecorder()
	mux.ServeHTTP(approved, approve)
	if approved.Code != http.StatusFound {
		t.Fatalf("approve status=%d body=%s", approved.Code, approved.Body.String())
	}
	redirect, err := url.Parse(approved.Header().Get("Location"))
	if err != nil || redirect.Query().Get("code") == "" || redirect.Query().Get("state") != "state-1" {
		t.Fatalf("invalid redirect: %s", approved.Header().Get("Location"))
	}

	tokenForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {redirect.Query().Get("code")}, "redirect_uri": {"https://client.example.com/callback"}, "code_verifier": {verifier}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	mux.ServeHTTP(tokenResponse, tokenRequest)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.Access == "" || tokens.Refresh == "" {
		t.Fatalf("missing tokens: %s", tokenResponse.Body.String())
	}
	if _, err := server.VerifyToken(context.Background(), tokens.Access, httptest.NewRequest(http.MethodPost, "/mcp", nil)); err != nil {
		t.Fatalf("verify token: %v", err)
	}
}

func TestAccessTokenRevokedWhenAccountDisabled(t *testing.T) {
	active := true
	var lookupErr error
	server, err := NewOAuthServer("https://mcp.example.com", "https://cores.example.com", strings.Repeat("s", 48), t.TempDir()+"/clients.json", func(_ context.Context, id uint) (bool, error) { return active && id == 7, lookupErr })
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.writeTokens(response, User{ID: 7}, "test-client")
	var tokens struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if _, err := server.VerifyToken(context.Background(), tokens.Access, request); err != nil {
		t.Fatal(err)
	}
	active = false
	if _, err := server.VerifyToken(context.Background(), tokens.Access, request); err == nil {
		t.Fatal("disabled account retained MCP access")
	}
	active, lookupErr = true, errors.New("database unavailable")
	if _, err := server.VerifyToken(context.Background(), tokens.Access, request); err == nil {
		t.Fatal("database failure did not fail closed")
	}
}

func TestAccessTokenRequiresExpiryAndNumericSubject(t *testing.T) {
	server, err := NewOAuthServer("https://mcp.example.com", "https://cores.example.com", strings.Repeat("s", 48), t.TempDir()+"/clients.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"7garbage", "0", "7"} {
		claims := tokenClaims{Type: "access", Scope: readScope, RegisteredClaims: jwtlib.RegisteredClaims{Subject: subject, Issuer: server.issuer, Audience: jwtlib.ClaimStrings{server.resource}}}
		if subject != "7" {
			claims.ExpiresAt = jwtlib.NewNumericDate(time.Now().Add(time.Hour))
		}
		raw, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(server.secret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.VerifyToken(context.Background(), raw, nil); err == nil {
			t.Fatalf("invalid claims accepted: %q", subject)
		}
	}
}

func TestOAuthLoginRetryKeepsAuthorizationQuery(t *testing.T) {
	server, err := NewOAuthServer("https://mcp.example.com", "https://cores.example.com", strings.Repeat("s", 48), t.TempDir()+"/clients.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.Register(mux)

	registration := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"token_endpoint_auth_method":"none"}`))
	registration.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	mux.ServeHTTP(registered, registration)
	var client struct {
		ID string `json:"client_id"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ID},
		"redirect_uri":          {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
		"scope":                 {readScope},
		"state":                 {"state-1"},
	}
	request := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", response.Code, response.Body.String())
	}
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Host != "cores.example.com" || !strings.HasPrefix(location.Query().Get("redirect"), "https://mcp.example.com/oauth/authorize?") {
		t.Fatalf("unexpected central login redirect: %s", location)
	}
}

func assertAuthorizationFields(t *testing.T, body string, values url.Values) {
	t.Helper()
	if !strings.Contains(body, `action="/oauth/authorize"`) || !strings.Contains(body, `href="/cores-theme.css"`) {
		t.Fatalf("consent page is missing the stable form action or suite theme: %s", body)
	}
	for name, candidates := range values {
		for _, value := range candidates {
			want := fmt.Sprintf(`name="%s" value="%s"`, name, template.HTMLEscapeString(value))
			if !strings.Contains(body, want) {
				t.Fatalf("consent page is missing %s: %s", name, body)
			}
		}
	}
}

func TestAuthenticateClient(t *testing.T) {
	public := oauthClient{AuthMethod: "none"}
	if !authenticateClient(public, "") || authenticateClient(public, "secret") {
		t.Fatal("public client authentication mismatch")
	}
	confidential := oauthClient{AuthMethod: "client_secret_post", SecretHash: hashSecret("secret")}
	if !authenticateClient(confidential, "secret") || authenticateClient(confidential, "wrong") {
		t.Fatal("confidential client authentication mismatch")
	}
}

func TestMalformedAuthorizationFormDoesNotRedirect(t *testing.T) {
	server, err := NewOAuthServer("https://mcp.example.com", "https://cores.example.com", strings.Repeat("s", 48), t.TempDir()+"/clients.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/oauth/authorize?redirect_uri=https://attacker.example/callback", strings.NewReader("%=broken"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusBadRequest)
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("unexpected redirect to %q", location)
	}
}

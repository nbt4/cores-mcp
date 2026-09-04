package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

const readScope = "cores:read"

type User struct {
	ID       uint
	Username string
	IsAdmin  bool
}

type UserValidator func(context.Context, uint) (bool, error)

type OAuthServer struct {
	issuer       string
	resource     string
	dashboardURL string
	secret       []byte
	clients      *clientStore
	validateUser UserValidator
	mu           sync.Mutex
	codes        map[string]authorizationCode
}

type authorizationCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Resource    string
	User        User
	ExpiresAt   time.Time
}

type tokenClaims struct {
	Scope    string `json:"scope"`
	Type     string `json:"token_type"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwtlib.RegisteredClaims
}

type suiteClaims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwtlib.RegisteredClaims
}

type oauthClient struct {
	ID           string    `json:"client_id"`
	Name         string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	AuthMethod   string    `json:"token_endpoint_auth_method"`
	SecretHash   string    `json:"client_secret_hash,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type clientStore struct {
	path    string
	mu      sync.RWMutex
	clients map[string]oauthClient
}

func NewOAuthServer(issuer, dashboardURL, secret, dataFile string, validateUser UserValidator) (*OAuthServer, error) {
	store, err := loadClientStore(dataFile)
	if err != nil {
		return nil, err
	}
	return &OAuthServer{
		issuer:       strings.TrimRight(issuer, "/"),
		resource:     strings.TrimRight(issuer, "/") + "/mcp",
		dashboardURL: strings.TrimRight(dashboardURL, "/"),
		secret:       []byte(secret),
		clients:      store,
		validateUser: validateUser,
		codes:        make(map[string]authorizationCode),
	}, nil
}

func (s *OAuthServer) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.metadata)
	mux.HandleFunc("POST /oauth/register", s.registerClient)
	mux.HandleFunc("GET /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/token", s.token)
}

func (s *OAuthServer) VerifyToken(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
	claims := &tokenClaims{}
	token, err := jwtlib.ParseWithClaims(raw, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, auth.ErrInvalidToken
		}
		return s.secret, nil
	}, jwtlib.WithAudience(s.resource), jwtlib.WithIssuer(s.issuer))
	if err != nil || !token.Valid || claims.Type != "access" || claims.Subject == "" {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{
		Scopes:     strings.Fields(claims.Scope),
		Expiration: claims.ExpiresAt.Time,
		UserID:     claims.Subject,
		Extra: map[string]any{
			"username": claims.Username,
			"is_admin": claims.IsAdmin,
		},
	}, nil
}

func StaticVerifier(tokens map[string]string) auth.TokenVerifier {
	return func(_ context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		name, ok := tokens[raw]
		if !ok {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{
			Scopes:     []string{readScope},
			Expiration: time.Now().Add(5 * time.Minute),
			UserID:     "service:" + name,
			Extra:      map[string]any{"username": name, "is_admin": false},
		}, nil
	}
}

func CombinedVerifier(primary auth.TokenVerifier, tokens map[string]string) auth.TokenVerifier {
	static := StaticVerifier(tokens)
	return func(ctx context.Context, raw string, req *http.Request) (*auth.TokenInfo, error) {
		if len(tokens) > 0 {
			if info, err := static(ctx, raw, req); err == nil {
				return info, nil
			}
		}
		return primary(ctx, raw, req)
	}
}

func ReadScope() string { return readScope }

func (s *OAuthServer) metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/oauth/authorize",
		"token_endpoint":                        s.issuer + "/oauth/token",
		"registration_endpoint":                 s.issuer + "/oauth/register",
		"scopes_supported":                      []string{readScope},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
	})
}

func (s *OAuthServer) registerClient(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name         string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		oauthError(w, "invalid_client_metadata", "invalid registration document", http.StatusBadRequest)
		return
	}
	if len(request.RedirectURIs) == 0 {
		oauthError(w, "invalid_redirect_uri", "at least one redirect URI is required", http.StatusBadRequest)
		return
	}
	for _, candidate := range request.RedirectURIs {
		if !validRedirectURI(candidate) {
			oauthError(w, "invalid_redirect_uri", "redirect URI must use HTTPS or loopback HTTP and have no fragment", http.StatusBadRequest)
			return
		}
	}
	if request.AuthMethod == "" {
		request.AuthMethod = "none"
	}
	if request.AuthMethod != "none" && request.AuthMethod != "client_secret_post" && request.AuthMethod != "client_secret_basic" {
		oauthError(w, "invalid_client_metadata", "unsupported token endpoint authentication method", http.StatusBadRequest)
		return
	}
	client := oauthClient{
		ID:           "cores_" + randomToken(24),
		Name:         strings.TrimSpace(request.Name),
		RedirectURIs: request.RedirectURIs,
		AuthMethod:   request.AuthMethod,
		CreatedAt:    time.Now().UTC(),
	}
	response := map[string]any{
		"client_id":                  client.ID,
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": client.AuthMethod,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if client.AuthMethod != "none" {
		secret := randomToken(32)
		client.SecretHash = hashSecret(secret)
		response["client_secret"] = secret
		response["client_secret_expires_at"] = 0
	}
	if err := s.clients.put(client); err != nil {
		oauthError(w, "server_error", "client registration could not be stored", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *OAuthServer) authorize(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			oauthError(w, "invalid_request", "invalid authorization form", http.StatusBadRequest)
			return
		}
		params = r.Form
	}
	client, err := s.validateAuthorizationRequest(params)
	if err != nil {
		oauthError(w, "invalid_request", err.Error(), http.StatusBadRequest)
		return
	}
	user, ok := s.sessionUser(r)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = loginTemplate.Execute(w, map[string]string{"Dashboard": s.dashboardURL, "Retry": r.URL.String()})
		return
	}
	if s.validateUser != nil {
		active, validateErr := s.validateUser(r.Context(), user.ID)
		if validateErr != nil || !active {
			oauthRedirectError(w, r, "access_denied", "Cores user is inactive or unavailable")
			return
		}
	}
	if r.Method == http.MethodGet {
		csrf := randomToken(24)
		http.SetCookie(w, &http.Cookie{Name: "cores_mcp_csrf", Value: csrf, Path: "/oauth/authorize", HttpOnly: true, Secure: strings.HasPrefix(s.issuer, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = consentTemplate.Execute(w, map[string]any{"Client": client.Name, "User": user.Username, "Values": params.Encode(), "CSRF": csrf})
		return
	}
	csrfCookie, csrfErr := r.Cookie("cores_mcp_csrf")
	if csrfErr != nil || csrfCookie.Value == "" || csrfCookie.Value != r.Form.Get("csrf") {
		oauthRedirectError(w, r, "access_denied", "authorization confirmation expired")
		return
	}
	if r.Form.Get("decision") != "allow" {
		oauthRedirectError(w, r, "access_denied", "user denied access")
		return
	}
	code := randomToken(32)
	s.mu.Lock()
	s.pruneCodesLocked()
	s.codes[code] = authorizationCode{
		ClientID: client.ID, RedirectURI: params.Get("redirect_uri"), Challenge: params.Get("code_challenge"),
		Resource: firstNonEmpty(params.Get("resource"), s.resource), User: user, ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()
	redirect, _ := url.Parse(params.Get("redirect_uri"))
	query := redirect.Query()
	query.Set("code", code)
	if state := params.Get("state"); state != "" {
		query.Set("state", state)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *OAuthServer) validateAuthorizationRequest(values url.Values) (oauthClient, error) {
	if values.Get("response_type") != "code" {
		return oauthClient{}, errors.New("only response_type=code is supported")
	}
	client, ok := s.clients.get(values.Get("client_id"))
	if !ok {
		return oauthClient{}, errors.New("unknown client_id")
	}
	if !contains(client.RedirectURIs, values.Get("redirect_uri")) {
		return oauthClient{}, errors.New("redirect_uri was not registered")
	}
	if values.Get("code_challenge_method") != "S256" || values.Get("code_challenge") == "" {
		return oauthClient{}, errors.New("PKCE with S256 is required")
	}
	if resource := values.Get("resource"); resource != "" && resource != s.resource {
		return oauthClient{}, errors.New("invalid resource")
	}
	for _, scope := range strings.Fields(values.Get("scope")) {
		if scope != readScope {
			return oauthClient{}, errors.New("unsupported scope")
		}
	}
	return client, nil
}

func (s *OAuthServer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request", "invalid token request", http.StatusBadRequest)
		return
	}
	clientID, clientSecret, hasBasic := r.BasicAuth()
	if !hasBasic {
		clientID, clientSecret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	client, ok := s.clients.get(clientID)
	if !ok || !authenticateClient(client, clientSecret) {
		oauthError(w, "invalid_client", "client authentication failed", http.StatusUnauthorized)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r, client)
	case "refresh_token":
		s.exchangeRefreshToken(w, r, client)
	default:
		oauthError(w, "unsupported_grant_type", "grant type is not supported", http.StatusBadRequest)
	}
}

func (s *OAuthServer) exchangeCode(w http.ResponseWriter, r *http.Request, client oauthClient) {
	codeValue := r.Form.Get("code")
	s.mu.Lock()
	code, ok := s.codes[codeValue]
	delete(s.codes, codeValue)
	s.mu.Unlock()
	if !ok || time.Now().After(code.ExpiresAt) || code.ClientID != client.ID || code.RedirectURI != r.Form.Get("redirect_uri") {
		oauthError(w, "invalid_grant", "authorization code is invalid or expired", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(digest[:]) != code.Challenge {
		oauthError(w, "invalid_grant", "PKCE verification failed", http.StatusBadRequest)
		return
	}
	s.writeTokens(w, code.User, client.ID)
}

func (s *OAuthServer) exchangeRefreshToken(w http.ResponseWriter, r *http.Request, client oauthClient) {
	claims := &tokenClaims{}
	token, err := jwtlib.ParseWithClaims(r.Form.Get("refresh_token"), claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, auth.ErrInvalidToken
		}
		return s.secret, nil
	}, jwtlib.WithAudience(s.issuer+"/oauth/token"), jwtlib.WithIssuer(s.issuer))
	if err != nil || !token.Valid || claims.Type != "refresh" || claims.Subject == "" || claims.ID != client.ID {
		oauthError(w, "invalid_grant", "refresh token is invalid or expired", http.StatusBadRequest)
		return
	}
	var userID uint
	if _, err := fmt.Sscanf(claims.Subject, "%d", &userID); err != nil {
		oauthError(w, "invalid_grant", "refresh token subject is invalid", http.StatusBadRequest)
		return
	}
	if s.validateUser != nil {
		active, validateErr := s.validateUser(r.Context(), userID)
		if validateErr != nil || !active {
			oauthError(w, "invalid_grant", "Cores user is inactive", http.StatusBadRequest)
			return
		}
	}
	s.writeTokens(w, User{ID: userID, Username: claims.Username, IsAdmin: claims.IsAdmin}, client.ID)
}

func (s *OAuthServer) writeTokens(w http.ResponseWriter, user User, clientID string) {
	now := time.Now().UTC()
	access, _ := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, tokenClaims{
		Scope: readScope, Type: "access", Username: user.Username, IsAdmin: user.IsAdmin,
		RegisteredClaims: jwtlib.RegisteredClaims{Issuer: s.issuer, Subject: fmt.Sprint(user.ID), Audience: jwtlib.ClaimStrings{s.resource}, IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Hour))},
	}).SignedString(s.secret)
	refresh, _ := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, tokenClaims{
		Scope: readScope, Type: "refresh", Username: user.Username, IsAdmin: user.IsAdmin,
		RegisteredClaims: jwtlib.RegisteredClaims{Issuer: s.issuer, Subject: fmt.Sprint(user.ID), Audience: jwtlib.ClaimStrings{s.issuer + "/oauth/token"}, ID: clientID, IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(30 * 24 * time.Hour))},
	}).SignedString(s.secret)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": refresh, "scope": readScope})
}

func (s *OAuthServer) sessionUser(r *http.Request) (User, bool) {
	cookie, err := r.Cookie("cores_token")
	if err != nil || cookie.Value == "" {
		return User{}, false
	}
	claims := &suiteClaims{}
	token, err := jwtlib.ParseWithClaims(cookie.Value, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, auth.ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid || claims.UserID == 0 {
		return User{}, false
	}
	return User{ID: claims.UserID, Username: claims.Username, IsAdmin: claims.IsAdmin}, true
}

func (s *OAuthServer) pruneCodesLocked() {
	now := time.Now()
	for key, code := range s.codes {
		if now.After(code.ExpiresAt) {
			delete(s.codes, key)
		}
	}
}

func loadClientStore(path string) (*clientStore, error) {
	store := &clientStore{path: path, clients: make(map[string]oauthClient)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &store.clients); err != nil {
		return nil, fmt.Errorf("decode OAuth client store: %w", err)
	}
	return store, nil
}

func (s *clientStore) get(id string) (oauthClient, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client, ok := s.clients[id]
	return client, ok
}

func (s *clientStore) put(client oauthClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.ID] = client
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.clients, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func validRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Fragment != "" || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := parsed.Hostname()
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func authenticateClient(client oauthClient, secret string) bool {
	if client.AuthMethod == "none" {
		return secret == ""
	}
	return client.SecretHash != "" && subtleCompare(hashSecret(secret), client.SecretHash)
}

func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var difference byte
	for i := range len(a) {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}

func hashSecret(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

func randomToken(size int) string {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func oauthRedirectError(w http.ResponseWriter, r *http.Request, code, description string) {
	redirectURI := firstNonEmpty(r.FormValue("redirect_uri"), r.URL.Query().Get("redirect_uri"))
	if !validRedirectURI(redirectURI) {
		oauthError(w, code, description, http.StatusBadRequest)
		return
	}
	redirect, _ := url.Parse(redirectURI)
	query := redirect.Query()
	query.Set("error", code)
	query.Set("error_description", description)
	if state := firstNonEmpty(r.FormValue("state"), r.URL.Query().Get("state")); state != "" {
		query.Set("state", state)
	}
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func oauthError(w http.ResponseWriter, code, description string, status int) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Cores MCP</title></head><body style="font-family:system-ui;max-width:42rem;margin:4rem auto;padding:1rem"><h1>Cores-Anmeldung erforderlich</h1><p>Melde dich zuerst im Cores Dashboard an. Kehre anschließend hierher zurück und versuche die Verbindung erneut.</p><p><a href="{{.Dashboard}}/login" target="_blank" rel="noopener">Cores Dashboard öffnen</a></p><p><a href="{{.Retry}}">Nach der Anmeldung erneut versuchen</a></p></body></html>`))

var consentTemplate = template.Must(template.New("consent").Parse(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Cores MCP freigeben</title></head><body style="font-family:system-ui;max-width:42rem;margin:4rem auto;padding:1rem"><h1>Cores MCP verbinden</h1><p><strong>{{.Client}}</strong> möchte im Namen von <strong>{{.User}}</strong> lesend auf freigegebene Cores-Daten zugreifen.</p><p>Die Verbindung kann Bestände, Jobs, Planungen und Beschaffungsinformationen lesen. Sie kann keine Daten verändern.</p><form method="post" action="/oauth/authorize?{{.Values}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><button name="decision" value="allow" type="submit">Lesenden Zugriff erlauben</button> <button name="decision" value="deny" type="submit">Ablehnen</button></form></body></html>`))

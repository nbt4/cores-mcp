package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/config"
)

type coreAPIClient struct {
	config config.Config
	client *http.Client
}

type suiteServiceClaims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwtlib.RegisteredClaims
}

func newCoreAPIClient(cfg config.Config) *coreAPIClient {
	return &coreAPIClient{config: cfg, client: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (c *coreAPIClient) doJSON(ctx context.Context, baseURL, path, method string, input, output any) error {
	token, err := c.suiteToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Core API request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), path)
	if err != nil {
		return fmt.Errorf("build Core API URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build Core API request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "cores_token", Value: token})
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("Core API unavailable: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read Core API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &problem)
		message := firstText(problem.Error, problem.Message, strings.TrimSpace(string(payload)), response.Status)
		return fmt.Errorf("Core API rejected request (%d): %s", response.StatusCode, message)
	}
	if output != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, output); err != nil {
			return fmt.Errorf("decode Core API response: %w", err)
		}
	}
	return nil
}

func (c *coreAPIClient) suiteToken(ctx context.Context) (string, error) {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil || !containsString(info.Scopes, "cores:write") {
		return "", errorsNew("cores:write permission is required; reconnect the MCP connector and grant create access")
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(info.UserID), 10, 32)
	if err != nil || parsed == 0 {
		return "", errorsNew("write tools require an interactive Cores user; service tokens cannot impersonate users")
	}
	username, _ := info.Extra["username"].(string)
	isAdmin, _ := info.Extra["is_admin"].(bool)
	now := time.Now().UTC()
	claims := suiteServiceClaims{
		UserID: uint(parsed), Username: username, IsAdmin: isAdmin,
		RegisteredClaims: jwtlib.RegisteredClaims{IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(2 * time.Minute))},
	}
	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString([]byte(c.config.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("sign Core API request: %w", err)
	}
	return token, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

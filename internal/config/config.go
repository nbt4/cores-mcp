package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address            string
	PublicURL          string
	DashboardURL       string
	DatabaseURL        string
	JWTSecret          string
	AuthMode           string
	StaticTokens       map[string]string
	OAuthDataFile      string
	AllowedOrigins     map[string]struct{}
	KnowledgeDirs      []string
	QueryTimeout       time.Duration
	MaxRows            int
	RateLimitPerMinute int
	RentalURL          string
	WarehouseURL       string
	PlannerURL         string
	ProcurementURL     string
	EnableWrites       bool
	TrustProxyHeaders  bool
}

func Load() (Config, error) {
	cfg := Config{
		Address:            env("ADDRESS", ":8090"),
		PublicURL:          strings.TrimRight(env("MCP_PUBLIC_URL", "http://localhost:8090"), "/"),
		DashboardURL:       strings.TrimRight(env("CORES_DASHBOARD_PUBLIC_URL", "http://localhost:8080"), "/"),
		JWTSecret:          strings.TrimSpace(os.Getenv("CORES_JWT_SECRET")),
		AuthMode:           strings.ToLower(env("MCP_AUTH_MODE", "oauth")),
		StaticTokens:       parseNamedSecrets(os.Getenv("MCP_STATIC_TOKENS")),
		OAuthDataFile:      env("MCP_OAUTH_DATA_FILE", "/var/lib/cores-mcp/oauth/clients.json"),
		AllowedOrigins:     parseSet(os.Getenv("MCP_ALLOWED_ORIGINS")),
		KnowledgeDirs:      splitClean(env("MCP_KNOWLEDGE_DIRS", "/knowledge")),
		QueryTimeout:       durationEnv("MCP_QUERY_TIMEOUT", 8*time.Second),
		MaxRows:            intEnv("MCP_MAX_ROWS", 200),
		RateLimitPerMinute: intEnv("MCP_RATE_LIMIT_PER_MINUTE", 120),
		RentalURL:          strings.TrimRight(env("RENTALCORE_URL", "http://rentalcore:8081"), "/"),
		WarehouseURL:       strings.TrimRight(env("WAREHOUSECORE_URL", "http://warehousecore:8082"), "/"),
		PlannerURL:         strings.TrimRight(env("PLANNERCORE_URL", "http://plannercore:8080"), "/"),
		ProcurementURL:     strings.TrimRight(env("PROCUREMENTCORE_URL", "http://procurementcore:8084"), "/"),
		EnableWrites:       boolEnv("MCP_ENABLE_WRITES", false),
		TrustProxyHeaders:  boolEnv("TRUST_PROXY_HEADERS", true),
	}

	if raw := strings.TrimSpace(os.Getenv("DATABASE_URL")); raw != "" {
		cfg.DatabaseURL = raw
	} else {
		host := env("DB_HOST", "postgres")
		port := env("DB_PORT", "5432")
		name := env("DB_NAME", "rentalcore")
		user := env("DB_USER", "rentalcore")
		password := os.Getenv("DB_PASSWORD")
		sslmode := env("DB_SSLMODE", "disable")
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, name, sslmode)
	}

	if cfg.AuthMode != "oauth" && cfg.AuthMode != "bearer" && cfg.AuthMode != "none" {
		return Config{}, fmt.Errorf("unsupported MCP_AUTH_MODE %q", cfg.AuthMode)
	}
	if cfg.AuthMode == "oauth" && len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("CORES_JWT_SECRET must contain at least 32 characters in oauth mode")
	}
	if cfg.AuthMode == "bearer" && len(cfg.StaticTokens) == 0 {
		return Config{}, errors.New("MCP_STATIC_TOKENS is required in bearer mode")
	}
	if cfg.EnableWrites && cfg.AuthMode != "oauth" {
		return Config{}, errors.New("MCP_ENABLE_WRITES requires MCP_AUTH_MODE=oauth so every change has a Cores user identity")
	}
	if cfg.MaxRows < 1 || cfg.MaxRows > 1000 {
		return Config{}, errors.New("MCP_MAX_ROWS must be between 1 and 1000")
	}
	return cfg, nil
}

func (c Config) MCPURL() string { return c.PublicURL + "/mcp" }

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value == 0 {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func splitClean(raw string) []string {
	var values []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func parseSet(raw string) map[string]struct{} {
	values := make(map[string]struct{})
	for _, value := range splitClean(raw) {
		values[strings.TrimRight(value, "/")] = struct{}{}
	}
	return values
}

func parseNamedSecrets(raw string) map[string]string {
	values := make(map[string]string)
	for _, item := range splitClean(raw) {
		name, secret, ok := strings.Cut(item, ":")
		if ok && strings.TrimSpace(name) != "" && strings.TrimSpace(secret) != "" {
			values[strings.TrimSpace(secret)] = strings.TrimSpace(name)
		}
	}
	return values
}

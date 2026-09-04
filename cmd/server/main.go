package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/nbt4/cores-mcp/internal/authn"
	"github.com/nbt4/cores-mcp/internal/config"
	"github.com/nbt4/cores-mcp/internal/httpx"
	"github.com/nbt4/cores-mcp/internal/mcpserver"
	"github.com/nbt4/cores-mcp/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	database, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	database.SetMaxOpenConns(12)
	database.SetMaxIdleConns(4)
	database.SetConnMaxLifetime(30 * time.Minute)
	repository := store.New(database, cfg.QueryTimeout, cfg.MaxRows)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := repository.Ping(ctx); err != nil {
		cancel()
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	cancel()

	protocolServer := mcpserver.New(cfg, repository, logger)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return protocolServer }, &mcp.StreamableHTTPOptions{Stateless: true})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "cores-mcp", "version": mcpserver.Version})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if err := repository.Ping(r.Context()); err != nil {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	mux.HandleFunc("GET /mcp/docs", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"service": "Cores MCP", "version": mcpserver.Version, "endpoint": cfg.MCPURL(),
			"transport": "Streamable HTTP", "access": "read-only", "scope": authn.ReadScope(),
			"documentation": "https://github.com/nbt4/cores-mcp#readme",
		})
	})

	metadata := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource: cfg.MCPURL(), AuthorizationServers: []string{cfg.PublicURL}, ScopesSupported: []string{authn.ReadScope()}, BearerMethodsSupported: []string{"header"},
		ResourceName: "Cores Suite", ResourceDocumentation: cfg.PublicURL + "/mcp/docs",
	})
	mux.Handle("GET /.well-known/oauth-protected-resource", metadata)
	mux.Handle("GET /.well-known/oauth-protected-resource/mcp", metadata)

	var protected http.Handler = mcpHandler
	switch cfg.AuthMode {
	case "oauth":
		oauthServer, oauthErr := authn.NewOAuthServer(cfg.PublicURL, cfg.DashboardURL, cfg.JWTSecret, cfg.OAuthDataFile, func(ctx context.Context, userID uint) (bool, error) {
			rows, queryErr := repository.Query(ctx, `SELECT is_active FROM users WHERE userid=$1 LIMIT 1`, userID)
			if queryErr != nil || len(rows) == 0 {
				return false, queryErr
			}
			active, _ := rows[0]["is_active"].(bool)
			return active, nil
		})
		if oauthErr != nil {
			logger.Error("initialize OAuth", "error", oauthErr)
			os.Exit(1)
		}
		oauthServer.Register(mux)
		verifier := authn.CombinedVerifier(oauthServer.VerifyToken, cfg.StaticTokens)
		protected = auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{Scopes: []string{authn.ReadScope()}, ResourceMetadataURL: cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp", ClockSkew: 30 * time.Second})(mcpHandler)
	case "bearer":
		protected = auth.RequireBearerToken(authn.StaticVerifier(cfg.StaticTokens), &auth.RequireBearerTokenOptions{Scopes: []string{authn.ReadScope()}, ResourceMetadataURL: cfg.PublicURL + "/.well-known/oauth-protected-resource/mcp"})(mcpHandler)
	case "none":
		logger.Warn("MCP authentication is disabled; use only in isolated development")
	}
	mux.Handle("/mcp", httpx.ValidateOrigin(cfg.PublicURL, cfg.AllowedOrigins, protected))

	limiter := httpx.NewRateLimiter(cfg.RateLimitPerMinute, cfg.TrustProxyHeaders)
	server := &http.Server{Addr: cfg.Address, Handler: httpx.Recover(logger, httpx.SecurityHeaders(cfg.PublicURL, limiter.Middleware(mux))), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}

	go func() {
		logger.Info("cores-mcp listening", "address", cfg.Address, "public_url", cfg.PublicURL, "auth_mode", cfg.AuthMode)
		if listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			logger.Error("http server failed", "error", listenErr)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

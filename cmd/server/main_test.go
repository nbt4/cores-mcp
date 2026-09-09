package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/nbt4/cores-mcp/internal/authn"
	"github.com/nbt4/cores-mcp/internal/config"
)

func TestBearerOptionsRequireEnabledWriteScope(t *testing.T) {
	tests := []struct {
		name         string
		enableWrites bool
		want         []string
	}{
		{name: "read only", want: []string{authn.ReadScope()}},
		{name: "guided writes", enableWrites: true, want: []string{authn.ReadScope(), authn.WriteScope()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := bearerOptions(config.Config{PublicURL: "https://cores.example.com", EnableWrites: test.enableWrites})
			if !slices.Equal(options.Scopes, test.want) {
				t.Fatalf("required scopes = %v, want %v", options.Scopes, test.want)
			}
			if options.ResourceMetadataURL != "https://cores.example.com/.well-known/oauth-protected-resource/mcp" {
				t.Fatalf("resource metadata URL = %q", options.ResourceMetadataURL)
			}
		})
	}
}

func TestWriteEnabledChallengeAdvertisesRequiredScopes(t *testing.T) {
	options := bearerOptions(config.Config{PublicURL: "https://cores.example.com", EnableWrites: true})
	verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
		return nil, auth.ErrInvalidToken
	}
	handler := auth.RequireBearerToken(verifier, options)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	challenge := recorder.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `scope="cores:read cores:write"`) {
		t.Fatalf("WWW-Authenticate = %q", challenge)
	}
}

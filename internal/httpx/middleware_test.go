package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersAllowCanonicalConsentResources(t *testing.T) {
	handler := SecurityHeaders("https://cores.tsunami-events.de/suite", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil))
	policy := response.Header().Get("Content-Security-Policy")
	for _, expected := range []string{
		"style-src 'self' https://fonts.googleapis.com",
		"font-src https://fonts.gstatic.com",
		"form-action 'self' https://cores.tsunami-events.de",
	} {
		if !strings.Contains(policy, expected) {
			t.Errorf("Content-Security-Policy %q does not contain %q", policy, expected)
		}
	}
}

func TestSecurityHeadersIgnoreInvalidPublicURL(t *testing.T) {
	policy := ContentSecurityPolicy("javascript:alert(1)")
	if !strings.Contains(policy, "form-action 'self';") || strings.Contains(policy, "javascript") {
		t.Fatalf("unexpected policy for invalid public URL: %q", policy)
	}
}

func TestContentSecurityPolicyAllowsOnlyValidRedirectOrigins(t *testing.T) {
	policy := ContentSecurityPolicy(
		"https://cores.tsunami-events.de",
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.ai/duplicate",
		"javascript:alert(1)",
	)
	if !strings.Contains(policy, "form-action 'self' https://cores.tsunami-events.de https://claude.ai;") {
		t.Fatalf("registered redirect origin missing from policy: %q", policy)
	}
	if strings.Contains(policy, "javascript") || strings.Count(policy, "https://claude.ai") != 1 {
		t.Fatalf("unsafe or duplicate redirect origin in policy: %q", policy)
	}
}

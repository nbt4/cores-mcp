package config

import "testing"

func TestParseNamedSecrets(t *testing.T) {
	got := parseNamedSecrets("claude:one, chatgpt:two,invalid")
	if got["one"] != "claude" || got["two"] != "chatgpt" || len(got) != 2 {
		t.Fatalf("unexpected tokens: %#v", got)
	}
}

func TestSplitClean(t *testing.T) {
	got := splitClean(" /one, ,/two ")
	if len(got) != 2 || got[0] != "/one" || got[1] != "/two" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestWritesRequireInteractiveOAuth(t *testing.T) {
	t.Setenv("MCP_AUTH_MODE", "none")
	t.Setenv("MCP_ENABLE_WRITES", "true")
	if _, err := Load(); err == nil {
		t.Fatal("write tools were enabled without interactive OAuth")
	}
}

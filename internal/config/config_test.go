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

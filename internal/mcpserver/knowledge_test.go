package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverKnowledge(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "strategy.md"), []byte("# Strategy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "secret.key"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := discoverKnowledge([]string{directory})
	if len(files) != 1 || files[0].Path != "strategy.md" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

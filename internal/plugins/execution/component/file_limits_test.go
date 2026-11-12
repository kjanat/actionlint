package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComponentLoadRejectsOversizedFileBeforeParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.wasm")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(128*1024*1024 + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	plugin, err := Load(t.Context(), path, Options{})
	if plugin != nil {
		_ = plugin.Close(t.Context())
	}
	if plugin != nil || err == nil || !strings.Contains(err.Error(), "artifact exceeds 134217728 byte limit") {
		t.Fatalf("oversized file reached component parsing: plugin=%v err=%v", plugin, err)
	}
}

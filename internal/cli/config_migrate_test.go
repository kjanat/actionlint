package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigMigrationCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	path := filepath.Join(root, "config.yaml")
	original := []byte("plugins: []\nconfig-variables: []\npolicy: {cache-operation: false}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	preview := pluginCommandTest(t, 0, "config", "migrate", "--config", path)
	if !strings.Contains(preview, "expression-reference") || !strings.Contains(preview, "presets and --strict") {
		t.Fatal(preview)
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, original) {
		t.Fatal("preview wrote config")
	}
	pluginCommandTest(t, 1, "config", "migrate", "--config", path, "--check")
	pluginCommandTest(t, 0, "config", "migrate", "--config", path, "--write")
	pluginCommandTest(t, 0, "config", "migrate", "--config", path, "--check")
	pluginCommandTest(t, 2, "config", "migrate", "--config", path, "--check", "--write")
}

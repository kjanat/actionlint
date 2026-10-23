package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestOutputCannotReplaceExplicitPlugin(t *testing.T) {
	data, err := os.ReadFile(pluginFixture("pipe-to-shell"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(pluginWorkflow, "check", "--no-config", "--plugin", path, "--yes", "--output-file", path, "-")
	if got.Status == 0 || !strings.Contains(got.Stderr, "also an input") {
		t.Fatalf("result=%+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatalf("plugin changed: %v", err)
	}
}

func TestOutputCannotReplaceConfiguredEngine(t *testing.T) {
	fixture := filepath.Join(componentTestdata, "..", "..", "core", "testdata", "engines", "echo.wasm")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	data, err = testutil.ReplaceWasmDataString(data, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "engine.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config := writeEngineConfig(t, path)
	got := runWithDefaultRuff(pythonWorkflow, "--config", config, "--yes", "--output-file", path, "-")
	if got.Status == 0 || !strings.Contains(got.Stderr, "also an input") {
		t.Fatalf("result=%+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatalf("engine changed: %v", err)
	}
}

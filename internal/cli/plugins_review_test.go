package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

func TestSharedPluginAndBuiltinLogWriter(t *testing.T) {
	var buffer bytes.Buffer
	writer := &synchronizedLogWriter{out: &buffer}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				if _, err := writer.Write([]byte("plugin or builtin\n")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if count := strings.Count(buffer.String(), "plugin or builtin\n"); count != 800 {
		t.Fatalf("lines=%d", count)
	}
}

func TestOutputCannotReplaceConfiguredEngine(t *testing.T) {
	fixture := filepath.Join(componentTestdata, "..", "..", "corewasm", "testdata", "engines", "echo.wasm")
	data, err := os.ReadFile(fixture)
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

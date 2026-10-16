package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/internal/pluginartifact"
)

func TestPluginInspectReadsMetadataWithoutExecution(t *testing.T) {
	// This module has an invalid code section. Static inspection succeeds, while
	// compilation or execution would fail; inspection must never start a runtime.
	data := append([]byte("\x00asm\x01\x00\x00\x00"), 10, 1, 255)
	metadata := `{"id":"inspect-only","engine":{"name":"ruff"}}`
	payload := append([]byte{byte(len(pluginartifact.MetadataSection))}, pluginartifact.MetadataSection...)
	payload = append(payload, metadata...)
	data = append(data, binary.AppendUvarint([]byte{0}, uint64(len(payload)))...)
	data = append(data, payload...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	got := testRunCommand("", "plugin", "inspect", server.URL, "--json")
	var info pluginartifact.Info
	if err := json.Unmarshal([]byte(got.Stdout), &info); err != nil || got.Status != 0 || string(info.Metadata) != metadata || info.Kind != pluginartifact.Core {
		t.Fatalf("inspection=%+v error=%v info=%+v", got, err, info)
	}
}

func TestConfiguredRemoteEngineAndComponent(t *testing.T) {
	var requests atomic.Int32
	module, err := os.ReadFile(filepath.Join(componentTestdata, "..", "..", "corewasm", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	component, err := os.ReadFile(filepath.Join(componentTestdata, "plugins", "no-sudo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/component" {
			_, _ = w.Write(component)
		} else {
			_, _ = w.Write(module)
		}
	}))
	defer server.Close()
	config := writeEngineConfig(t, server.URL+"/echo")
	got := runWithDefaultRuff(pythonWorkflow, "--config", config, "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "F821") {
		t.Fatalf("remote backend: %+v", got)
	}
	workflow := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo true\n"
	got = testRunCommand(workflow, "check", "--no-config", "--plugin", server.URL+"/component", "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "sudo") {
		t.Fatalf("remote component: %+v", got)
	}
	if requests.Load() != 2 {
		t.Fatalf("fetched more than once per invocation: %d", requests.Load())
	}
}

func TestRealRemoteRuffArtifact(t *testing.T) {
	// Opt in to the separately built Ruff artifact. CI unit tests above use
	// checked-in engines; this test verifies the actual published ABI and section.
	path := os.Getenv("ACTIONLINT_TEST_RUFF_WASM")
	if path == "" {
		t.Skip("set ACTIONLINT_TEST_RUFF_WASM to a packaged Ruff artifact")
	}
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	uri := fmt.Sprintf("%s/ruff#sha256=%x", server.URL, sha256.Sum256(data))
	got := testRunCommand("", "plugin", "inspect", uri, "--json")
	if got.Status != 0 || !strings.Contains(got.Stdout, `"wasm-core-json"`) {
		t.Fatalf("Ruff custom section: %+v", got)
	}
	got = runWithDefaultRuff(pythonWorkflow, "--no-config", "--plugin", uri, "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "<stdin>:6:20:") || !strings.Contains(got.Stdout, "F821") {
		t.Fatalf("Ruff remote plugin: %+v", got)
	}
	config := writeEngineConfig(t, uri)
	got = runWithDefaultRuff(pythonWorkflow, "--config", config, "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "<stdin>:6:20:") || !strings.Contains(got.Stdout, "F821") {
		t.Fatalf("Ruff remote config: %+v", got)
	}
}

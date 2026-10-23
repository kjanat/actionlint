package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	"actionlint.kjanat.dev/v2/internal/testutil"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
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
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)
	var requests atomic.Int32
	module, err := os.ReadFile(filepath.Join(componentTestdata, "..", "..", "core", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	module, err = testutil.ReplaceWasmDataString(module, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
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
	config := writeEngineConfig(t, fmt.Sprintf("%s/echo#sha256=%x", server.URL, sha256.Sum256(module)))
	got := runWithDefaultRuff(pythonWorkflow, "--config", config, "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "F821") {
		t.Fatalf("remote backend: %+v", got)
	}
	workflow := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo true\n"
	source := fmt.Sprintf("%s/component#sha256=%x", server.URL, sha256.Sum256(component))
	got = testRunCommand(workflow, "check", "--no-config", "--plugin", source, "-")
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

func TestRealRuffEngineConfiguration(t *testing.T) {
	module := os.Getenv("ACTIONLINT_TEST_RUFF_WASM")
	if module == "" {
		t.Skip("set ACTIONLINT_TEST_RUFF_WASM to a packaged Ruff artifact")
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	const workflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          if True:\n            print(1)\n        shell: python\n"
	for _, command := range [][]string{nil, {"check"}} {
		for _, configured := range []bool{false, true} {
			for _, tc := range []struct {
				settings, want string
				status         int
			}{
				{"config: {preview: true}, select: [E111]", "E111", 1},
				{"select: [XYZ]", "Unknown rule selector", 3},
				{"target-version: py399", "unknown variant", 3},
			} {
				settings := tc.settings
				if configured {
					settings += ", backend: {wasm: {path: " + strconv.Quote(module) + "}}"
				}
				content := "lint: {rules: {external: {ruff: error}}}\ntools: {ruff: {" + settings + "}}\n"
				if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				args := append([]string{"actionlint"}, command...)
				args = append(args, "--config="+config, "--no-shellcheck", "--no-color", "--summary=false")
				if !configured {
					args = append(args, "--plugin="+module)
				}
				var stdout, stderr bytes.Buffer
				cmd := cli.Command{Stdin: strings.NewReader(workflow), Stdout: &stdout, Stderr: &stderr}
				status := cmd.Main(append(args, "-"))
				if status != tc.status || !strings.Contains(stdout.String()+stderr.String(), tc.want) {
					t.Fatalf("command=%v configured=%v settings=%s: exit=%d stdout=%s stderr=%s", command, configured, tc.settings, status, &stdout, &stderr)
				}
			}
		}
	}
}

func TestRealShellcheckComponentConfiguration(t *testing.T) {
	module := os.Getenv("ACTIONLINT_TEST_SHELLCHECK_WASM")
	if module == "" {
		t.Skip("set ACTIONLINT_TEST_SHELLCHECK_WASM to a packaged ShellCheck component")
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	const workflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $HOME\n        shell: bash\n"
	for _, tc := range []struct {
		options, want string
		status        int
	}{
		{"{}", "SC2086", 1},
		{"{disable: [SC2086]}", "", 0},
		{"{shel: sh}", "unknown field", 3},
	} {
		content := "lint: {rules: {preset: none, plugins: {shellcheck-rs: {level: error, options: " + tc.options + "}}}}\n"
		if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got := testRunCommand(workflow, "check", "--config="+config, "--plugin="+module, "-")
		if got.Status != tc.status || !strings.Contains(got.Stdout+got.Stderr, tc.want) || tc.status == 0 && got.Stdout != "" {
			t.Fatalf("options=%s: %+v", tc.options, got)
		}
	}
}

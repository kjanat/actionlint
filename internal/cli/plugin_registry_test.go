package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/testutil"
)

func pluginCommandTest(t *testing.T, want int, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	command := Command{Stdin: strings.NewReader(""), Stdout: &output, Stderr: &output}
	status := command.MainContext(t.Context(), append([]string{"actionlint"}, args...))
	if status != want {
		t.Fatalf("%v: status %d, want %d: %s", args, status, want, &output)
	}
	return output.String()
}

func TestRegistryCLI(t *testing.T) {
	help := pluginCommandTest(t, 0, "plugin", "install", "--help")
	for _, flag := range []string{"--version", "--registry", "--plugin-lock"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("help lacks %s: %s", flag, help)
		}
	}
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.9.0", wasm)
	r.Add(t, "v1.10.0", wasm)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	search := pluginCommandTest(t, 0, "plugin", "search", "sudo", "--registry", testutil.FixtureRegistryURL, "--json")
	if !strings.Contains(search, testutil.FixturePluginName) {
		t.Fatal(search)
	}
	pluginCommandTest(t, 0, "plugin", "versions", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL)
	pluginCommandTest(t, 0, "plugin", "install", testutil.FixturePluginName, "--version", "v1.9.0", "--registry", testutil.FixtureRegistryURL)
	lockPath := filepath.Join(root, ".github", pluginregistry.LockName)
	readPin := func() pluginregistry.Pin {
		t.Helper()
		f, err := pluginregistry.OpenLock(t.Context(), lockPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Lock.Plugins) != 1 {
			t.Fatalf("pins: %+v", f.Lock)
		}
		return f.Lock.Plugins[0]
	}
	if readPin().Version != "v1.9.0" {
		t.Fatal("exact version ignored")
	}
	r.Offline = true
	requests := len(r.Requests)
	pluginCommandTest(t, 0, "plugin", "sync")
	pluginCommandTest(t, 0, "plugin", "list", "--json")
	if output := pluginCommandTest(t, 1, "check", "--no-ruff", "--no-shellcheck", "workflow.yml"); !strings.Contains(output, "[no-sudo]") {
		t.Fatal(output)
	}
	if output := pluginCommandTest(t, 0, "rules", "no-sudo"); !strings.Contains(output, "no-sudo") {
		t.Fatal(output)
	}
	pluginCommandTest(t, 0, "check", "--no-config", "--no-ruff", "--no-shellcheck", "workflow.yml")
	pluginCommandTest(t, 1, "check", "--no-config", "--plugin-lock", lockPath, "--no-ruff", "--no-shellcheck", "workflow.yml")
	if len(r.Requests) != requests {
		t.Fatal("pinned offline commands contacted network")
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 3, "check", "--no-ruff", "--no-shellcheck", "--yes", "--output-file", lockPath, "workflow.yml")
	after, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("output overwrote lock")
	}
	t.Setenv("ACTIONLINT_NO_CONFIG", "true")
	output := pluginCommandTest(t, 0, "rules", "--json")
	if strings.Contains(output, `"name": "no-sudo"`) {
		t.Fatal("rules ignored no-config environment")
	}
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	r.Offline = false
	pluginCommandTest(t, 0, "plugin", "update")
	if readPin().Version != "v1.9.0" {
		t.Fatal("update changed exact selector")
	}
	pluginCommandTest(t, 0, "plugin", "update", testutil.FixturePluginName, "--version", "latest")
	if readPin().Version != "v1.10.0" {
		t.Fatal("latest sorted lexically")
	}
	before, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	r.Add(t, "v1.11.0", wasm)
	delete(r.Files, "https://"+testutil.FixturePluginName+"/releases/download/v1.11.0/plugin.wasm")
	pluginCommandTest(t, 3, "plugin", "update")
	after, err = os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed download changed lock")
	}
	r.Publish(t, "v1.9.0")
	pluginCommandTest(t, 3, "plugin", "update")
	if readPin().Version != "v1.10.0" {
		t.Fatal("stale catalogue downgraded pin")
	}
	pluginCommandTest(t, 0, "plugin", "update", testutil.FixturePluginName, "--version", "v1.9.0")
	// A fresh checkout/cache restores from the lock alone, including when the
	// catalogue itself is missing. It never reselects latest during sync.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	delete(r.Files, testutil.FixtureRegistryURL+"index.json")
	pluginCommandTest(t, 0, "plugin", "sync")
	r.Offline = true
	pluginCommandTest(t, 1, "check", "--no-ruff", "--no-shellcheck", "workflow.yml")
	pluginCommandTest(t, 0, "plugin", "remove", testutil.FixturePluginName)
	pluginCommandTest(t, 0, "check", "--no-ruff", "--no-shellcheck", "workflow.yml")
}

func TestRegistryInstallDoesNotExecutePlugin(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	// Valid container/metadata, but no analyzer or provider exports. Runtime
	// loading must fail; installation must succeed without starting a guest.
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Chdir(t.TempDir())
	lock := filepath.Join(t.TempDir(), pluginregistry.LockName)
	output := pluginCommandTest(t, 0, "plugin", "install", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL, "--plugin-lock", lock, "--json")
	var saved pluginregistry.Lock
	if err := json.Unmarshal([]byte(output), &saved); err != nil || len(saved.Plugins) != 1 {
		t.Fatalf("%s: %v", output, err)
	}
	pluginCommandTest(t, 2, "rules", "--plugin-lock", lock)
}

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/pluginref"
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
	for _, flag := range []string{"--version", "--registry", "--plugin-lock", "--config"} {
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
	t.Setenv("HOME", t.TempDir())
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
	pluginCommandTest(t, 0, "plugin", "versions", "publisher/plugin", "--registry", testutil.FixtureRegistryURL)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.9.0", "--registry", testutil.FixtureRegistryURL)
	lockPath := filepath.Join(root, ".github", pluginregistry.LockName)
	configPath := filepath.Join(root, ".github", "actionlint.yaml")
	readDeclarations := func() repoconfig.PluginDeclarations {
		t.Helper()
		cfg, err := repoconfig.Load(configPath, "no-sudo")
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Plugins
	}
	if refs := readDeclarations(); len(refs) != 1 || refs[0] != (pluginref.Reference{Name: testutil.FixturePluginName, Version: "v1.9.0", Registry: testutil.FixtureRegistryURL}) {
		t.Fatalf("install did not declare exact dependency: %#v", refs)
	}
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
	pluginCommandTest(t, 0, "plugin", "install")
	if len(r.Requests) != requests {
		t.Fatal("matching installation contacted registry")
	}
	syncOutput := pluginCommandTest(t, 0, "plugin", "sync")
	if len(r.Requests) != requests+1 || r.Requests[requests] != testutil.FixtureRegistryURL+"statuses.json" || !strings.Contains(syncOutput, "using cached status") {
		t.Fatalf("offline sync should attempt status refresh only: %v %s", r.Requests[requests:], syncOutput)
	}
	requests = len(r.Requests)
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
	if refs := readDeclarations(); len(refs) != 1 || refs[0].Version != "latest" {
		t.Fatalf("update did not persist requested selector: %#v", refs)
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
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
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("failed download changed configuration")
	}
	r.Publish(t, "v1.9.0")
	pluginCommandTest(t, 3, "plugin", "update")
	if readPin().Version != "v1.10.0" {
		t.Fatal("stale catalogue downgraded pin")
	}
	pluginCommandTest(t, 0, "plugin", "update", testutil.FixturePluginName, "--version", "v1.9.0")
	// A fresh checkout/cache restores declared pins without registry metadata, even when the
	// catalogue itself is missing. It never reselects latest during sync.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	delete(r.Files, testutil.FixtureRegistryURL+"index.json")
	pluginCommandTest(t, 0, "plugin", "sync")
	r.Offline = true
	pluginCommandTest(t, 1, "check", "--no-ruff", "--no-shellcheck", "workflow.yml")
	pluginCommandTest(t, 0, "plugin", "remove", "publisher/plugin")
	if refs := readDeclarations(); refs == nil || len(refs) != 0 {
		t.Fatalf("removal must persist explicit empty plugins: %#v", refs)
	}
	pluginCommandTest(t, 0, "check", "--no-ruff", "--no-shellcheck", "workflow.yml")
}

func TestRegistryManifestInstallAndMigration(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	if err := os.WriteFile("custom.yaml", []byte("# keep this comment\nplugins:\n  - name: publisher/plugin\n    version: v1.0.0\n    registry: "+testutil.FixtureRegistryURL+"\nlint:\n  rules:\n    plugins:\n      no-sudo: warn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "--config", "custom.yaml")
	content, err := os.ReadFile("custom.yaml")
	if err != nil || !strings.Contains(string(content), "# keep this comment") || !strings.Contains(string(content), "no-sudo: warn") {
		t.Fatalf("install changed unrelated configuration: %s, %v", content, err)
	}
	r.Offline = true
	requests := len(r.Requests)
	pluginCommandTest(t, 0, "plugin", "install", "--config", "custom.yaml")
	// The lock remains usable for explicit low-level loading; normal management
	// requires a declaration and provides one explicit migration operation.
	if err := os.Remove("custom.yaml"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"list", "sync", "update"} {
		output := pluginCommandTest(t, 3, "plugin", operation, "--config", "custom.yaml")
		if !strings.Contains(output, "actionlint plugin install") {
			t.Fatalf("missing migration guidance: %s", output)
		}
	}
	pluginCommandTest(t, 0, "plugin", "install", "--config", "custom.yaml")
	cfg, err := repoconfig.Load("custom.yaml")
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != testutil.FixturePluginName {
		t.Fatalf("lock migration did not declare plugin: %#v, %v", cfg, err)
	}
	if len(r.Requests) != requests {
		t.Fatal("migration/repeated installation contacted registry despite exact cached lock")
	}
	// Environment selection must target the same project for management as checking.
	t.Setenv("ACTIONLINT_CONFIG", "custom.yaml")
	pluginCommandTest(t, 0, "plugin", "list")
	t.Setenv("ACTIONLINT_NO_CONFIG", "true")
	pluginCommandTest(t, 2, "plugin", "list")
	pluginCommandTest(t, 0, "plugin", "list", "--config", "custom.yaml", "--no-config=false")
}

func TestRegistryManagementArgumentErrors(t *testing.T) {
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"install", "--no-config"},
		{"update", "--no-config"},
		{"remove", "publisher/plugin", "--no-config"},
		{"sync", "--no-config"},
		{"list", "--no-config"},
		{"install", "--version", "v1"},
		{"install", "--registry", testutil.FixtureRegistryURL},
		{"update", "--version", "v1"},
	} {
		pluginCommandTest(t, 2, append([]string{"plugin"}, args...)...)
	}
}

func TestRequestedPlugin(t *testing.T) {
	existing := []pluginref.Reference{{Name: testutil.FixturePluginName, Version: "v1", Registry: testutil.FixtureRegistryURL}}
	for _, tc := range []struct {
		name    string
		options pluginInvocation
		version string
	}{
		{"Publisher/Plugin", pluginInvocation{}, "v1"},
		{"publisher/plugin@v2", pluginInvocation{}, "v2"},
		{"publisher/plugin", pluginInvocation{Version: "latest"}, "latest"},
	} {
		ref, err := requestedPlugin(tc.name, tc.options, existing)
		if err != nil || ref.Name != testutil.FixturePluginName || ref.Registry != testutil.FixtureRegistryURL || ref.Version != tc.version {
			t.Fatalf("request %q: %#v, %v", tc.name, ref, err)
		}
	}
	if _, err := requestedPlugin("publisher/plugin@v1", pluginInvocation{Version: "v2"}, existing); err == nil {
		t.Fatal("conflicting tag and --version accepted")
	}
}

func TestRegistryInstallDoesNotExecutePlugin(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	// Valid container/metadata, but no analyzer or provider exports. Runtime
	// loading must fail; installation must succeed without starting a guest.
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	lock := filepath.Join(t.TempDir(), pluginregistry.LockName)
	output := pluginCommandTest(t, 0, "plugin", "install", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL, "--plugin-lock", lock, "--json")
	var saved pluginregistry.Lock
	if err := json.Unmarshal([]byte(output), &saved); err != nil || len(saved.Plugins) != 1 {
		t.Fatalf("%s: %v", output, err)
	}
	pluginCommandTest(t, 2, "rules", "--plugin-lock", lock)
}

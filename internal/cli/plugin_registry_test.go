package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"

	fixtureenvironment "actionlint.kjanat.dev/v2/internal/development/tests/support/environment"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
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
	wasm, err := os.ReadFile("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.9.0", wasm)
	r.Add(t, "v1.10.0", wasm)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Setenv("ACTIONLINT_CONFIG", "")
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\npermissions: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	search := pluginCommandTest(t, 0, "plugin", "search", "sudo", "--registry", fixtureregistry.FixtureRegistryURL, "--json")
	if !strings.Contains(search, fixtureregistry.FixturePluginName) {
		t.Fatal(search)
	}
	pluginCommandTest(t, 0, "plugin", "versions", "publisher/plugin", "--registry", fixtureregistry.FixtureRegistryURL)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.9.0", "--registry", fixtureregistry.FixtureRegistryURL)
	lockPath := filepath.Join(root, ".github", plugininstall.LockName)
	configPath := filepath.Join(root, ".github", "actionlint.yaml")
	readDeclarations := func() repoconfig.PluginDeclarations {
		t.Helper()
		cfg, err := checking.ConfigurationRules().Load(configPath, "no-sudo")
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Plugins
	}
	if refs := readDeclarations(); len(refs) != 1 || refs[0] != testReference(t, fixtureregistry.FixturePluginName, "v1.9.0", fixtureregistry.FixtureRegistryURL) {
		t.Fatalf("install did not declare exact dependency: %#v", refs)
	}
	readPin := func() plugininstall.Pin {
		t.Helper()
		refs := readDeclarations()
		if len(refs) != 1 {
			t.Fatalf("declarations: %+v", refs)
		}
		client, err := plugininstall.New(refs[0].Registry())
		if err != nil {
			t.Fatal(err)
		}
		pin, err := client.Lookup(t.Context(), refs[0].Name(), refs[0].Version())
		if err != nil {
			t.Fatal(err)
		}
		return pin
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("installation created a lockfile: %v", err)
	}
	if readPin().Version != "v1.9.0" {
		t.Fatal("exact version ignored")
	}
	r.SetOffline(true)
	requests := len(r.Requests())
	pluginCommandTest(t, 0, "plugin", "install")
	if len(r.Requests()) != requests {
		t.Fatal("matching installation contacted registry")
	}
	syncOutput := pluginCommandTest(t, 0, "plugin", "sync")
	if len(r.Requests()) != requests+1 || r.Requests()[requests] != fixtureregistry.FixtureRegistryURL+"statuses.json" || !strings.Contains(syncOutput, "using cached status") {
		t.Fatalf("offline sync should attempt status refresh only: %v %s", r.Requests()[requests:], syncOutput)
	}
	requests = len(r.Requests())
	pluginCommandTest(t, 0, "plugin", "list", "--json")
	if output := pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml"); !strings.Contains(output, "[no-sudo]") {
		t.Fatal(output)
	}
	if output := pluginCommandTest(t, 0, "rules", "no-sudo"); !strings.Contains(output, "no-sudo") {
		t.Fatal(output)
	}
	pluginCommandTest(t, 0, "check", "--no-config", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if len(r.Requests()) != requests {
		t.Fatal("pinned offline commands contacted network")
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 3, "check", "--no-tool=ruff", "--no-tool=shellcheck", "--yes", "--output-file", configPath, "workflow.yml")
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("output overwrote plugin configuration")
	}
	t.Setenv("ACTIONLINT_NO_CONFIG", "true")
	output := pluginCommandTest(t, 0, "rules", "--json")
	if strings.Contains(output, `"name": "no-sudo"`) {
		t.Fatal("rules ignored no-config environment")
	}
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	r.SetOffline(false)
	pluginCommandTest(t, 0, "plugin", "update")
	if readPin().Version != "v1.9.0" {
		t.Fatal("update changed exact selector")
	}
	pluginCommandTest(t, 0, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "latest")
	if readPin().Version != "v1.10.0" {
		t.Fatal("latest sorted lexically")
	}
	if refs := readDeclarations(); len(refs) != 1 || refs[0].Version() != "v1.10.0" {
		t.Fatalf("update did not persist exact release: %#v", refs)
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	r.Add(t, "v1.11.0", wasm)
	r.DeleteFile("https://" + fixtureregistry.FixturePluginName + "/releases/download/v1.11.0/plugin.wasm")
	pluginCommandTest(t, 3, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "latest")
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("failed download changed configuration")
	}
	r.Publish(t, "v1.9.0")
	pluginCommandTest(t, 3, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "latest")
	if readPin().Version != "v1.10.0" {
		t.Fatal("stale catalogue downgraded pin")
	}
	pluginCommandTest(t, 0, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "v1.9.0")
	// A cold host needs the trusted registry record; a warm host then runs offline.
	catalogue := r.File(fixtureregistry.FixtureRegistryURL + "index.json")
	fixtureenvironment.IsolateUserCache(t)
	r.DeleteFile(fixtureregistry.FixtureRegistryURL + "index.json")
	pluginCommandTest(t, 3, "plugin", "sync")
	r.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", catalogue)
	pluginCommandTest(t, 0, "plugin", "sync")
	r.SetOffline(true)
	pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	pluginCommandTest(t, 0, "plugin", "remove", "publisher/plugin")
	if refs := readDeclarations(); refs == nil || len(refs) != 0 {
		t.Fatalf("removal must persist explicit empty plugins: %#v", refs)
	}
	pluginCommandTest(t, 0, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
}

func TestRegistryManifestInstallAndMissingConfiguration(t *testing.T) {
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	if err := os.WriteFile("custom.yaml", []byte("# keep this comment\nplugins:\n  - name: publisher/plugin\n    version: v1.0.0\n    registry: "+fixtureregistry.FixtureRegistryURL+"\nlint:\n  rules:\n    plugins:\n      no-sudo: warn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "--config", "custom.yaml")
	content, err := os.ReadFile("custom.yaml")
	if err != nil || !strings.Contains(string(content), "# keep this comment") || !strings.Contains(string(content), "no-sudo: warn") {
		t.Fatalf("install changed unrelated configuration: %s, %v", content, err)
	}
	r.SetOffline(true)
	requests := len(r.Requests())
	pluginCommandTest(t, 0, "plugin", "install", "--config", "custom.yaml")
	if err := os.Remove("custom.yaml"); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"list", "sync", "update", "install"} {
		output := pluginCommandTest(t, 0, "plugin", operation, "--config", "custom.yaml")
		if !strings.Contains(output, "No plugins installed.") {
			t.Fatalf("cached undeclared plugin activated: %s", output)
		}
	}
	if _, err := os.Stat("custom.yaml"); !os.IsNotExist(err) {
		t.Fatalf("missing config was recreated from the host cache: %v", err)
	}
	if len(r.Requests()) != requests {
		t.Fatal("undeclared cache entries triggered registry requests")
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
		{"install", "--registry", fixtureregistry.FixtureRegistryURL},
		{"update", "--version", "v1"},
		{"search", "--registry", "http://example.com"},
		{"versions", "publisher/plugin", "--registry", "http://example.com"},
	} {
		pluginCommandTest(t, 2, append([]string{"plugin"}, args...)...)
	}
}

func TestRequestedPlugin(t *testing.T) {
	t.Parallel()
	existing := []pluginref.Reference{testReference(t, fixtureregistry.FixturePluginName, "v1", fixtureregistry.FixtureRegistryURL)}
	for _, tc := range []struct {
		name    string
		options plugincmd.RegistryOptions
		version string
	}{
		{"Publisher/Plugin", plugincmd.RegistryOptions{}, "v1"},
		{"publisher/plugin@v2", plugincmd.RegistryOptions{}, "v2"},
		{"publisher/plugin", plugincmd.RegistryOptions{Version: "latest"}, "latest"},
	} {
		ref, err := plugincmd.RequestedPlugin(tc.name, tc.options, existing)
		if err != nil || ref.Name() != fixtureregistry.FixturePluginName || ref.Registry() != fixtureregistry.FixtureRegistryURL || ref.Version() != tc.version {
			t.Fatalf("request %q: %#v, %v", tc.name, ref, err)
		}
	}
	if _, err := plugincmd.RequestedPlugin("publisher/plugin@v1", plugincmd.RegistryOptions{Version: "v2"}, existing); err == nil {
		t.Fatal("conflicting tag and --version accepted")
	}
}

func TestRegistryInstallDoesNotExecutePlugin(t *testing.T) {
	r := fixtureregistry.NewPluginRegistry(t)
	// Valid container/metadata, but no analyzer or provider exports. Runtime
	// loading must fail; installation must succeed without starting a guest.
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	t.Chdir(t.TempDir())
	lock := filepath.Join(t.TempDir(), plugininstall.LockName)
	output := pluginCommandTest(t, 0, "plugin", "install", fixtureregistry.FixturePluginName, "--registry", fixtureregistry.FixtureRegistryURL, "--plugin-lock", lock, "--json")
	var saved plugininstall.Lock
	if err := json.Unmarshal([]byte(output), &saved); err != nil || len(saved.Plugins) != 1 {
		t.Fatalf("%s: %v", output, err)
	}
	pluginCommandTest(t, 3, "rules", "--plugin-lock", lock)
}

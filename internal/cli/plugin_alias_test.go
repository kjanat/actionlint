package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// testReference builds a declaration through pluginref.New and fails the test
// when name, version or registry is invalid.
func testReference(t *testing.T, name, version, registry string) pluginref.Reference {
	t.Helper()
	ref, err := pluginref.New(name, version, registry)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRegistryAliasCommandScope(t *testing.T) {
	t.Parallel()
	refs := []pluginref.Reference{
		testReference(t, "check", "latest", pluginref.DefaultRegistry),
		testReference(t, "check", "v1.0.0", fixtureregistry.FixtureRegistryURL),
	}
	project := &plugininstall.Project{Lock: &plugininstall.LockFile{Lock: plugininstall.Lock{
		Plugins: []plugininstall.Pin{
			{Name: "github.com/first/plugin", Alias: "check", Registry: pluginref.DefaultRegistry},
			{Name: "github.com/second/plugin", Alias: "check", Registry: fixtureregistry.FixtureRegistryURL},
		},
	}}}
	if _, err := plugincmd.DeclaredPlugin(t.Context(), project, refs, "check", ""); err == nil {
		t.Fatal("ambiguous registry alias accepted")
	}
	for _, tc := range []struct {
		name, registry string
		index          int
	}{
		{"check", pluginref.DefaultRegistry, 0},
		{"check", fixtureregistry.FixtureRegistryURL, 1},
		{"github.com/second/plugin", "", 1},
	} {
		index, err := plugincmd.DeclaredPlugin(t.Context(), project, refs, tc.name, tc.registry)
		if err != nil || index != tc.index {
			t.Fatalf("%+v: index=%d error=%v", tc, index, err)
		}
	}
	if _, err := plugincmd.RequestedPlugin("check", plugincmd.RegistryOptions{}, refs); err == nil {
		t.Fatal("install selected ambiguous alias registry")
	}
	ref, err := plugincmd.RequestedPlugin("check", plugincmd.RegistryOptions{Registry: fixtureregistry.FixtureRegistryURL}, refs)
	if err != nil || ref.Registry() != fixtureregistry.FixtureRegistryURL || ref.Version() != "v1.0.0" {
		t.Fatalf("scoped install: %+v %v", ref, err)
	}
}

func TestRegistryAliasInstallOfflineAndIdentity(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetAliases("community-check")
	registry.Add(t, "v1.0.0", wasm)
	registry.Add(t, "v1.1.0", wasm)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo test}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "community-check@v1.0.0", "--registry", fixtureregistry.FixtureRegistryURL)
	configPath := filepath.Join(".github", "actionlint.yaml")
	lockPath := filepath.Join(".github", plugininstall.LockName)
	cfg, err := checking.ConfigurationRules().Load(configPath)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName {
		t.Fatalf("canonical declaration lost: %+v %v", cfg, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("alias installation created a project lock: %v", err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	pluginCommandTest(t, 0, "plugin", "install", "community-check")
	pluginCommandTest(t, 0, "plugin", "install", "community-check@v1.0.0")
	pluginCommandTest(t, 0, "plugin", "install")
	pluginCommandTest(t, 0, "plugin", "list")
	pluginCommandTest(t, 1, "check", "--no-tool=shellcheck", "--no-tool=ruff", "workflow.yml")
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline alias triggered registry lookup: %v", requests)
	}
	registry.SetOffline(false)
	pluginCommandTest(t, 0, "plugin", "install", "community-check@latest")
	cfg, err = checking.ConfigurationRules().Load(configPath)
	if err != nil || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("explicit latest alias did not update its existing declaration: %+v, %v", cfg, err)
	}
	pluginCommandTest(t, 0, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "v1.1.0")
	cfg, err = checking.ConfigurationRules().Load(configPath)
	if err != nil || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("canonical update lost exact declaration: %+v %v", cfg, err)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", fixtureregistry.FixturePluginName, "--registry", fixtureregistry.FixtureRegistryURL)
	registry.SetRepository("github.com/another/plugin", 20)
	registry.Add(t, "v1.1.0", wasm)
	registry.Publish(t, "v1.1.0")
	output := pluginCommandTest(t, 3, "plugin", "update", "community-check")
	if !strings.Contains(output, "not found") {
		t.Fatalf("canonical package followed reassigned alias: %s", output)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(beforeConfig, after) {
		t.Fatal("failed resolution changed canonical configuration")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("update created a lockfile: %v", err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	pluginCommandTest(t, 0, "plugin", "remove", fixtureregistry.FixturePluginName)
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("canonical removal of alias contacted registry: %v", requests)
	}
}

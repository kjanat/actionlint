package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/plugincommand"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

// testReference builds a declaration through pluginreference.New and fails the test
// when name, version or registry is invalid.
func testReference(t *testing.T, name, version, registry string) pluginreference.Reference {
	t.Helper()
	ref, err := pluginreference.New(name, version, registry)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRegistryAliasCommandScope(t *testing.T) {
	t.Parallel()
	refs := []pluginreference.Reference{
		testReference(t, "check", "latest", pluginreference.DefaultRegistry),
		testReference(t, "check", "v1.0.0", fixtureregistry.FixtureRegistryURL),
	}
	project := &plugininstall.Project{Releases: pluginregistry.ResolvedPlugins{
		Plugins: []pluginregistry.Pin{
			{Name: "github.com/first/plugin", Alias: "check", Registry: pluginreference.DefaultRegistry},
			{Name: "github.com/second/plugin", Alias: "check", Registry: fixtureregistry.FixtureRegistryURL},
		},
	}}
	if _, err := plugincommand.DeclaredPlugin(t.Context(), project, refs, "check", ""); err == nil {
		t.Fatal("ambiguous registry alias accepted")
	}
	for _, tc := range []struct {
		name, registry string
		index          int
	}{
		{"check", pluginreference.DefaultRegistry, 0},
		{"check", fixtureregistry.FixtureRegistryURL, 1},
		{"github.com/second/plugin", "", 1},
	} {
		index, err := plugincommand.DeclaredPlugin(t.Context(), project, refs, tc.name, tc.registry)
		if err != nil || index != tc.index {
			t.Fatalf("%+v: index=%d error=%v", tc, index, err)
		}
	}
	if _, err := plugincommand.RequestedPlugin("check", plugincommand.RegistryOptions{}, refs); err == nil {
		t.Fatal("install selected ambiguous alias registry")
	}
	ref, err := plugincommand.RequestedPlugin("check", plugincommand.RegistryOptions{Registry: fixtureregistry.FixtureRegistryURL}, refs)
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
	acquisition := pluginacquisition.Settings{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
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
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "community-check@v1.0.0", "--registry", fixtureregistry.FixtureRegistryURL)
	configPath := filepath.Join(".github", "actionlint.yaml")
	lockPath := filepath.Join(".github", "actionlint.plugins.lock.json")
	cfg, err := checking.ConfigurationRules().LoadConfig(configPath)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName {
		t.Fatalf("canonical declaration lost: %+v %v", cfg, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("alias installation created a project lock: %v", err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "community-check")
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "community-check@v1.0.0")
	pluginCommandTest(t, acquisition, 0, "plugin", "install")
	pluginCommandTest(t, acquisition, 0, "plugin", "list")
	pluginCommandTest(t, acquisition, 1, "check", "--no-tool=shellcheck", "--no-tool=ruff", "workflow.yml")
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline alias triggered registry lookup: %v", requests)
	}
	registry.SetOffline(false)
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "community-check@latest")
	cfg, err = checking.ConfigurationRules().LoadConfig(configPath)
	if err != nil || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("explicit latest alias did not update its existing declaration: %+v, %v", cfg, err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "update", fixtureregistry.FixturePluginName, "--version", "v1.1.0")
	cfg, err = checking.ConfigurationRules().LoadConfig(configPath)
	if err != nil || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("canonical update lost exact declaration: %+v %v", cfg, err)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 0, "plugin", "install", fixtureregistry.FixturePluginName, "--registry", fixtureregistry.FixtureRegistryURL)
	registry.SetRepository("github.com/another/plugin", 20)
	registry.Add(t, "v1.1.0", wasm)
	registry.Publish(t, "v1.1.0")
	output := pluginCommandTest(t, acquisition, 3, "plugin", "update", "community-check")
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
	pluginCommandTest(t, acquisition, 0, "plugin", "remove", fixtureregistry.FixturePluginName)
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("canonical removal of alias contacted registry: %v", requests)
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
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
	refs := []pluginref.Reference{
		testReference(t, "check", "latest", pluginref.DefaultRegistry),
		testReference(t, "check", "v1.0.0", testutil.FixtureRegistryURL),
	}
	project := &localapp.PluginProject{Lock: &pluginregistry.LockFile{Lock: pluginregistry.Lock{
		Plugins: []pluginregistry.Pin{
			{Name: "github.com/first/plugin", Alias: "check", Registry: pluginref.DefaultRegistry},
			{Name: "github.com/second/plugin", Alias: "check", Registry: testutil.FixtureRegistryURL},
		},
	}}}
	if _, err := declaredPlugin(t.Context(), project, refs, "check", ""); err == nil {
		t.Fatal("ambiguous registry alias accepted")
	}
	for _, tc := range []struct {
		name, registry string
		index          int
	}{
		{"check", pluginref.DefaultRegistry, 0},
		{"check", testutil.FixtureRegistryURL, 1},
		{"github.com/second/plugin", "", 1},
	} {
		index, err := declaredPlugin(t.Context(), project, refs, tc.name, tc.registry)
		if err != nil || index != tc.index {
			t.Fatalf("%+v: index=%d error=%v", tc, index, err)
		}
	}
	if _, err := requestedPlugin("check", pluginInvocation{}, refs); err == nil {
		t.Fatal("install selected ambiguous alias registry")
	}
	ref, err := requestedPlugin("check", pluginInvocation{Registry: testutil.FixtureRegistryURL}, refs)
	if err != nil || ref.Registry() != testutil.FixtureRegistryURL || ref.Version() != "v1.0.0" {
		t.Fatalf("scoped install: %+v %v", ref, err)
	}
}

func TestRegistryAliasInstallOfflineAndIdentity(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := testutil.NewPluginRegistry(t)
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
	pluginCommandTest(t, 0, "plugin", "install", "community-check@v1.0.0", "--registry", testutil.FixtureRegistryURL)
	configPath := filepath.Join(".github", "actionlint.yaml")
	lockPath := filepath.Join(".github", pluginregistry.LockName)
	cfg, err := config.Load(configPath)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != testutil.FixturePluginName {
		t.Fatalf("canonical declaration lost: %+v %v", cfg, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("alias installation created a project lock: %v", err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	pluginCommandTest(t, 0, "plugin", "install")
	pluginCommandTest(t, 0, "plugin", "list")
	pluginCommandTest(t, 1, "check", "--no-tool=shellcheck", "--no-tool=ruff", "workflow.yml")
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline alias triggered registry lookup: %v", requests)
	}
	registry.SetOffline(false)
	pluginCommandTest(t, 0, "plugin", "update", testutil.FixturePluginName, "--version", "v1.1.0")
	cfg, err = config.Load(configPath)
	if err != nil || cfg.Plugins[0].Name() != testutil.FixturePluginName || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("canonical update lost exact declaration: %+v %v", cfg, err)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL)
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
	pluginCommandTest(t, 0, "plugin", "remove", testutil.FixturePluginName)
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("canonical removal of alias contacted registry: %v", requests)
	}
}

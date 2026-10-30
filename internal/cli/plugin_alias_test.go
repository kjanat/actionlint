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

func TestRegistryAliasCommandScope(t *testing.T) {
	refs := []pluginref.Reference{
		{Name: "check", Registry: pluginref.DefaultRegistry, Version: "latest"},
		{Name: "check", Registry: testutil.FixtureRegistryURL, Version: "v1.0.0"},
	}
	project := &localapp.PluginProject{Lock: &pluginregistry.LockFile{Lock: pluginregistry.Lock{
		Plugins: []pluginregistry.Pin{
			{Name: "github.com/first/plugin", Alias: "check", Registry: pluginref.DefaultRegistry},
			{Name: "github.com/second/plugin", Alias: "check", Registry: testutil.FixtureRegistryURL},
		},
	}}}
	if _, err := declaredPlugin(project, refs, "check", ""); err == nil {
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
		index, err := declaredPlugin(project, refs, tc.name, tc.registry)
		if err != nil || index != tc.index {
			t.Fatalf("%+v: index=%d error=%v", tc, index, err)
		}
	}
	if _, err := requestedPlugin("check", pluginInvocation{}, refs); err == nil {
		t.Fatal("install selected ambiguous alias registry")
	}
	ref, err := requestedPlugin("check", pluginInvocation{Registry: testutil.FixtureRegistryURL}, refs)
	if err != nil || ref.Registry != testutil.FixtureRegistryURL || ref.Version != "v1.0.0" {
		t.Fatalf("scoped install: %+v %v", ref, err)
	}
}

func TestRegistryAliasInstallOfflineAndIdentity(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := testutil.NewPluginRegistry(t)
	registry.Aliases = []string{"community-check"}
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
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != "community-check" {
		t.Fatalf("alias declaration lost: %+v %v", cfg, err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0].Alias != "community-check" || lock.Lock.Plugins[0].Name != testutil.FixturePluginName {
		t.Fatalf("canonical pin missing: %+v %v", lock, err)
	}
	registry.Requests, registry.Offline = nil, true
	pluginCommandTest(t, 0, "plugin", "install")
	pluginCommandTest(t, 0, "plugin", "list")
	pluginCommandTest(t, 1, "check", "--no-tool=shellcheck", "--no-tool=ruff", "workflow.yml")
	if len(registry.Requests) != 0 {
		t.Fatalf("offline alias triggered registry lookup: %v", registry.Requests)
	}
	registry.Offline = false
	pluginCommandTest(t, 0, "plugin", "update", testutil.FixturePluginName, "--version", "v1.1.0")
	cfg, err = config.Load(configPath)
	if err != nil || cfg.Plugins[0].Name != "community-check" || cfg.Plugins[0].Version != "v1.1.0" {
		t.Fatalf("canonical update replaced alias declaration: %+v %v", cfg, err)
	}
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output := pluginCommandTest(t, 3, "plugin", "install", testutil.FixturePluginName, "--registry", testutil.FixtureRegistryURL)
	if !strings.Contains(output, "duplicate") {
		t.Fatalf("alias and full repository not rejected: %s", output)
	}
	registry.Name, registry.RepositoryID = "github.com/another/plugin", 20
	registry.Add(t, "v1.1.0", wasm)
	registry.Publish(t, "v1.1.0")
	output = pluginCommandTest(t, 3, "plugin", "update", "community-check")
	if !strings.Contains(output, "alias") || !strings.Contains(output, "remove explicitly") {
		t.Fatalf("alias reassignment not rejected: %s", output)
	}
	for path, before := range map[string][]byte{configPath: beforeConfig, lockPath: beforeLock} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("failed resolution changed %s: %v", path, err)
		}
	}
	registry.Requests, registry.Offline = nil, true
	pluginCommandTest(t, 0, "plugin", "remove", testutil.FixturePluginName)
	if len(registry.Requests) != 0 {
		t.Fatalf("canonical removal of alias contacted registry: %v", registry.Requests)
	}
}

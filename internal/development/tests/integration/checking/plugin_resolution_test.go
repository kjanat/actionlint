package checking_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestRegistryProjectRunsWithoutLockfile(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	registry := fixtureregistry.NewPluginRegistry(t)
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("plugins:\n  - name: " + fixtureregistry.FixturePluginName + "\n    version: v1.0.0\n    registry: " + fixtureregistry.FixtureRegistryURL + "\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	sources, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil || len(sources.Paths) != 1 {
		t.Fatalf("exact config required a lockfile: %+v, %v", sources, err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != 0 {
		t.Fatal("warm-cache checking contacted the registry")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("checking rewrote configuration")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), plugininstall.LockName)); !os.IsNotExist(err) {
		t.Fatalf("checking created a lockfile: %v", err)
	}
}

func TestPluginInstallStoresCanonicalExactConfigOnly(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetAliases("community-check")
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), repositoryfixture.Directory(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	request, err := pluginref.New("community-check", "latest", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{request}, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{request}); err != nil {
		t.Fatal(err)
	}
	cfg, err := checking.ConfigurationRules().Load(project.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("installation saved unresolved intent: %+v", cfg.Plugins)
	}
	if _, err := os.Stat(project.Lock.Path); !os.IsNotExist(err) {
		t.Fatalf("installation created a project lockfile: %v", err)
	}
}

func TestRegistryUpdateWithoutLockPreservesPublisherIdentity(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	root := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pluginref.New(fixtureregistry.FixturePluginName, "v1.0.0", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{initial}, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{initial}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(project.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetRepository(fixtureregistry.FixturePluginName, 99)
	registry.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	project, err = plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := pluginref.New(fixtureregistry.FixturePluginName, "v2.0.0", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{updated}, []pluginref.Reference{updated}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("publisher change accepted: %v", err)
	}
	after, err := os.ReadFile(project.Manifest.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected update changed configuration")
	}
}

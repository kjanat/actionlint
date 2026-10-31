package localapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRegistryProjectRunsWithoutLockfile(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("plugins:\n  - name: " + testutil.FixturePluginName + "\n    version: v1.0.0\n    registry: " + testutil.FixtureRegistryURL + "\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	sources, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Repository})
	if err != nil || len(sources.Paths) != 1 {
		t.Fatalf("exact config required a lockfile: %+v, %v", sources, err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	if _, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Repository}); err != nil {
		t.Fatal(err)
	}
	if len(registry.Requests()) != 0 {
		t.Fatal("warm-cache checking contacted the registry")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("checking rewrote configuration")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), pluginregistry.LockName)); !os.IsNotExist(err) {
		t.Fatalf("checking created a lockfile: %v", err)
	}
}

func TestPluginInstallStoresCanonicalExactConfigOnly(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.SetAliases("community-check")
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	project, err := OpenPluginProject(t.Context(), pluginRepository(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	request, err := pluginref.New("community-check", "latest", testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{request}, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{request}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(project.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != testutil.FixturePluginName || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("installation saved unresolved intent: %+v", cfg.Plugins)
	}
	if _, err := os.Stat(project.Lock.Path); !os.IsNotExist(err) {
		t.Fatalf("installation created a project lockfile: %v", err)
	}
}

func TestRegistryUpdateWithoutLockPreservesPublisherIdentity(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := pluginref.New(testutil.FixturePluginName, "v1.0.0", testutil.FixtureRegistryURL)
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
	registry.SetRepository(testutil.FixturePluginName, 99)
	registry.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	project, err = OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := pluginref.New(testutil.FixturePluginName, "v2.0.0", testutil.FixtureRegistryURL)
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

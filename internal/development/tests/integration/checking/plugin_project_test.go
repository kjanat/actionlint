package checking_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"

	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"

	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"

	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

func TestPluginProjectDeclarationsAuthorizeLoading(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := registryAcquisition(registry)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginreference.New(pin.Name, pin.Version, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginreference.Reference{ref}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	project, err = installation.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedPlugins(t.Context(), project.Manifest.References)
	if err != nil || len(selected.Plugins) != 1 || !selected.Plugins[0].SameRelease(pin) {
		t.Fatalf("configured release changed: %+v %v", selected, err)
	}
	declared := project.Manifest.References[0]
	project.Manifest.References[0], err = pluginreference.New(declared.Name(), "v2.0.0", declared.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.SelectedPlugins(t.Context(), project.Manifest.References); err == nil || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("changed declaration accepted stale pin: %v", err)
	}
	if err := os.WriteFile(project.Manifest.Path, []byte("plugins: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	sources, err := (pluginselection.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).Resolve(t.Context(), root, checkrequest.Request{Kind: checkrequest.Repository})
	if err != nil || len(sources.Paths) != 0 || len(registry.Requests()) != requests {
		t.Fatalf("undeclared plugin activated: %+v %v", sources, err)
	}
}

func TestDirectDeclarationRequiresNoRegistryLock(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), pluginacquisition.Options{}, root, "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginreference.ParsePluginReference("https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginreference.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	project, err = installation.OpenProject(t.Context(), checking.ConfigurationRules(), pluginacquisition.Options{}, root, "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := project.SelectedPlugins(t.Context(), project.Manifest.References)
	if err != nil || len(selected.Plugins) != 0 {
		t.Fatalf("direct source acquired a registry pin: %+v %v", selected, err)
	}
	if got := project.DirectSources(); len(got) != 1 || got[0] != ref.DirectSource() {
		t.Fatalf("direct source lost: %v", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(project.Manifest.Path), "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
		t.Fatal("created an unnecessary lockfile for an already pinned URL")
	}
}

func TestPluginProjectMissingPinsAndForeignScope(t *testing.T) {
	t.Parallel()
	root, foreign := repositoryfixture.Directory(t), repositoryfixture.Directory(t)
	if err := os.MkdirAll(filepath.Join(foreign, ".github"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, ".github", "actionlint.yaml"), []byte("plugins: [owner/plugin]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := checkrequest.Request{Kind: checkrequest.Repository}
	if _, err := (pluginselection.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), foreign, input); err == nil || !strings.Contains(err.Error(), "plugin install") {
		t.Fatalf("missing lock ignored: %v", err)
	}
	sources, err := (pluginselection.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := sources.ValidateScope(t.Context(), checking.ConfigurationRules(), []string{filepath.Join(foreign, "workflow.yml")}); err == nil {
		t.Fatal("foreign declarations ignored by language server")
	}
}

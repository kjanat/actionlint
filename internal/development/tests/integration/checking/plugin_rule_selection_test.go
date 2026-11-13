package checking_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestRuleSelectionIgnoresUnrelatedConfigAndKeepsDependencies(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	registry := fixtureregistry.NewPluginRegistry(t)
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := plugininstall.Sync(t.Context(), project.Lock.Lock); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(filepath.Dir(project.Manifest.Path), "base.yml")
	fixturefiles.WriteText(t, base, "dependency: &plugin\n  name: "+fixtureregistry.FixturePluginName+"\n  registry: "+fixtureregistry.FixtureRegistryURL+"\nplugins: [*plugin]\npolicy: {bad: true}\n")
	fixturefiles.WriteText(t, project.Manifest.Path, "extends: [base.yml]\ntools: {shellcheck: {enabled: wrong}}\n")
	registry.ResetRequests()
	registry.SetOffline(true)
	for _, selection := range []plugininstall.Selection{{}, {ConfigFile: project.Manifest.Path}} {
		sources, err := selection.ResolveForRules(t.Context(), root)
		if err != nil || len(sources.Paths) != 1 || sources.Paths[0] != pin.Source() {
			t.Fatalf("inherited plugin lost: %+v %v", sources, err)
		}
		rules, err := checking.RulesWithPlugins(t.Context(), sources)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, rule := range rules {
			found = found || rule.Name == "no-sudo"
		}
		if !found {
			t.Fatal("inherited plugin rule missing")
		}
		if _, err := selection.Select(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}); err == nil {
			t.Fatal("ordinary checking ignored invalid config")
		}
	}
	fixturefiles.WriteText(t, project.Manifest.Path, "extends: [base.yml]\nplugins: []\npolicy: {bad: true}\n")
	sources, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).ResolveForRules(t.Context(), root)
	if err != nil || len(sources.Paths) != 0 {
		t.Fatalf("leaf opt-out lost: %+v %v", sources, err)
	}
	if len(registry.Requests()) != 0 {
		t.Fatalf("rule discovery resolved registry: %v", registry.Requests())
	}
}

func TestRuleSelectionRejectsDependencyErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, base, leaf, want string
	}{
		{"invalid inherited plugins", "plugins: [42]\n", "extends: [base.yml]\nplugins: []\n", "plugins"},
		{"missing base", "", "extends: [missing.yml]\n", "missing.yml"},
		{"cycle", "extends: [actionlint.yaml]\n", "extends: [base.yml]\n", "cyclic"},
		{"malformed document", "", "policy: [\n", "config"},
		{"missing pin", "", "plugins: [community-check]\n", "plugin install"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := repositoryfixture.Directory(t)
			directory := filepath.Join(root, ".github")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.base != "" {
				fixturefiles.WriteText(t, filepath.Join(directory, "base.yml"), tc.base)
			}
			fixturefiles.WriteText(t, filepath.Join(directory, "actionlint.yaml"), tc.leaf)
			if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).ResolveForRules(t.Context(), root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("dependency error lost: %v; want %q", err, tc.want)
			}
		})
	}
	root := repositoryfixture.Directory(t)
	selection := plugininstall.Selection{ConfigFile: filepath.Join(root, "missing.yml"), RuleSet: checking.ConfigurationRules()}
	if _, err := selection.ResolveForRules(t.Context(), root); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing explicit config accepted: %v", err)
	}
}

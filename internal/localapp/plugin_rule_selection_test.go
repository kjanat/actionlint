package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestRuleSelectionIgnoresUnrelatedConfigAndKeepsDependencies(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), project.Lock.Lock); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(filepath.Dir(project.Manifest.Path), "base.yml")
	writeManifestTest(t, base, "dependency: &plugin\n  name: "+testutil.FixturePluginName+"\n  registry: "+testutil.FixtureRegistryURL+"\nplugins: [*plugin]\npolicy: {bad: true}\n")
	writeManifestTest(t, project.Manifest.Path, "extends: [base.yml]\ntools: {shellcheck: {enabled: wrong}}\n")
	registry.Requests, registry.Offline = nil, true
	for _, selection := range []PluginSelection{{}, {ConfigFile: project.Manifest.Path}} {
		sources, err := selection.ResolveForRules(t.Context(), root)
		if err != nil || len(sources.Paths) != 1 || sources.Paths[0] != pin.Source() {
			t.Fatalf("inherited plugin lost: %+v %v", sources, err)
		}
		rules, err := RulesSources(t.Context(), sources)
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
		if _, err := selection.Select(t.Context(), root, application.Input{Kind: application.Repository}); err == nil {
			t.Fatal("ordinary checking ignored invalid config")
		}
	}
	writeManifestTest(t, project.Manifest.Path, "extends: [base.yml]\nplugins: []\npolicy: {bad: true}\n")
	sources, err := (PluginSelection{}).ResolveForRules(t.Context(), root)
	if err != nil || len(sources.Paths) != 0 {
		t.Fatalf("leaf opt-out lost: %+v %v", sources, err)
	}
	if len(registry.Requests) != 0 {
		t.Fatalf("rule discovery resolved registry: %v", registry.Requests)
	}
}

func TestRuleSelectionRejectsDependencyErrors(t *testing.T) {
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
			root := pluginRepository(t)
			directory := filepath.Join(root, ".github")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if tc.base != "" {
				writeManifestTest(t, filepath.Join(directory, "base.yml"), tc.base)
			}
			writeManifestTest(t, filepath.Join(directory, "actionlint.yaml"), tc.leaf)
			if _, err := (PluginSelection{}).ResolveForRules(t.Context(), root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("dependency error lost: %v; want %q", err, tc.want)
			}
		})
	}
	root := pluginRepository(t)
	selection := PluginSelection{ConfigFile: filepath.Join(root, "missing.yml")}
	if _, err := selection.ResolveForRules(t.Context(), root); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing explicit config accepted: %v", err)
	}
}

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestDependencySettingsInheritanceAndOverrides(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for name, body := range map[string]string{
		"base.toml":       "[lint.dependencies]\nfail-on-all = true\nfail-on-categories = ['security']\nfail-on-rules = ['example/CODE']\n",
		"actionlint.toml": "extends = ['base.toml']\n[lint.dependencies]\nfail-on-all = false\nfail-on-categories = []\n[[overrides]]\nincludes = ['release.yml']\n[overrides.lint.dependencies]\nfail-on-all = true\nfail-on-rules = []\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "security", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := rules.ReadContext(t.Context(), filepath.Join(root, "actionlint.toml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file     string
		all      bool
		selected DependencyRules
	}{
		{"ci.yml", false, DependencyRules{"example/CODE"}},
		{"release.yml", true, DependencyRules{}},
	} {
		for name, resolve := range map[string]func(*Config, string, string) (*Config, error){"workflow": rules.ForFile, "action": rules.ForAction} {
			cfg, err := resolve(loaded.Config, filepath.Join(root, tc.file), root)
			if err != nil {
				t.Fatal(err)
			}
			d := cfg.Lint.Dependencies
			if d.FailOnAll == nil || *d.FailOnAll != tc.all || len(d.FailOnCategories) != 0 || !reflect.DeepEqual(d.FailOnRules, tc.selected) {
				t.Errorf("%s %s: got %+v, want all=%v rules=%v", name, tc.file, d, tc.all, tc.selected)
			}
		}
	}
}

func TestDependencySettingsRejectInvalidSelections(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "security", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		"fail-on-all: nope", "fail-on-categories: [typo]", "fail-on-rules: [missing]",
		"fail-on-rules: ['example/']", "fail-on-rules: ['example/with space']", "fail-on-rules: ['example/code/more']", "unknown: true",
	} {
		if _, err := rules.Parse([]byte("lint: {dependencies: {" + body + "}}")); err == nil {
			t.Errorf("accepted invalid dependency settings: %s", body)
		}
	}
}

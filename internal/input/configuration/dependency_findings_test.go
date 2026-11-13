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

func TestProgrammaticDependencyOverrides(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "security", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		selection  DependencyFindings
		categories DependencyCategories
		rules      DependencyRules
	}{
		{"omitted", DependencyFindings{}, DependencyCategories{"security"}, DependencyRules{"example/CODE"}},
		{"clear categories", DependencyFindings{FailOnCategories: DependencyCategories{}}, DependencyCategories{}, DependencyRules{"example/CODE"}},
		{"clear rules", DependencyFindings{FailOnRules: DependencyRules{}}, DependencyCategories{"security"}, DependencyRules{}},
		{"clear both", DependencyFindings{FailOnCategories: DependencyCategories{}, FailOnRules: DependencyRules{}}, DependencyCategories{}, DependencyRules{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			all, overrideAll := true, false
			tc.selection.FailOnAll = &overrideAll
			config := &Config{
				Lint:      LintConfig{Dependencies: DependencyFindings{FailOnAll: &all, FailOnCategories: DependencyCategories{"security"}, FailOnRules: DependencyRules{"example/CODE"}}},
				Overrides: []Override{{Includes: []string{"release.yml"}, Lint: &LintConfig{Dependencies: tc.selection}}},
			}
			for name, resolve := range map[string]func(*Config, string, string) (*Config, error){"workflow": rules.ForFile, "action": rules.ForAction} {
				selected, err := resolve(config, "release.yml", "")
				if err != nil {
					t.Fatal(err)
				}
				got := selected.Lint.Dependencies
				if got.FailOnAll == nil || *got.FailOnAll || !reflect.DeepEqual(got.FailOnCategories, tc.categories) || !reflect.DeepEqual(got.FailOnRules, tc.rules) {
					t.Errorf("%s: got %+v, want all=false categories=%v rules=%v", name, got, tc.categories, tc.rules)
				}
			}
		})
	}
}

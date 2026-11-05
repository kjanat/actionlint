package config_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestCatalogEntriesAreUniqueAndGrouped(t *testing.T) {
	groups := (&repoconfig.LintRulesConfig{}).Groups()
	seen := map[string]bool{}
	for _, rule := range checking.ConfigurationRules().Rules() {
		if seen[rule.Name] {
			t.Errorf("duplicate rule %q", rule.Name)
		}
		seen[rule.Name] = true
		if _, ok := groups[rule.Category]; !ok {
			t.Errorf("rule %q uses unknown category %q", rule.Name, rule.Category)
		}
		if rule.Description == "" {
			t.Errorf("rule %q has no description", rule.Name)
		}
		if rule.Options != nil && rule.Configure == nil {
			t.Errorf("rule %q declares options without a policy projection", rule.Name)
		}
	}
	if !checking.ConfigurationRules().Suppressible("shellcheck") || checking.ConfigurationRules().Suppressible("unknown-rule") {
		t.Fatal("suppressible lookup does not follow the catalog")
	}
}

func TestCatalogMetadataIsDetached(t *testing.T) {
	first, ok := checking.ConfigurationRules().FindRule("script-injection")
	if !ok || len(first.Examples) == 0 {
		t.Fatal("script-injection examples missing")
	}
	want := first.Examples[0].Before
	first.Examples[0].Before = "changed by caller"
	for _, listed := range checking.ConfigurationRules().Rules() {
		if listed.Name == "script-injection" {
			if listed.Examples[0].Before != want {
				t.Fatal("lookup changed the listed catalog")
			}
			listed.Examples[0].Before = "changed by another caller"
		}
	}
	last, ok := checking.ConfigurationRules().FindRule("script-injection")
	if !ok || last.Examples[0].Before != want {
		t.Fatal("catalog retained caller-owned example storage")
	}
}

func BenchmarkFindRule(b *testing.B) {
	for _, name := range []string{"cache-operation", "script-injection"} {
		b.Run(name, func(b *testing.B) {
			if _, ok := checking.ConfigurationRules().FindRule(name); !ok {
				b.Fatal("missing rule", name)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := checking.ConfigurationRules().FindRule(name); !ok {
					b.Fatal("missing rule", name)
				}
			}
		})
	}
}

package config

import (
	"testing"
)

func TestCatalogEntriesAreUniqueAndGrouped(t *testing.T) {
	groups := (&LintRulesConfig{}).Groups()
	seen := map[string]bool{}
	for _, rule := range Rules() {
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
	if !Suppressible("shellcheck") || Suppressible("unknown-rule") {
		t.Fatal("suppressible lookup does not follow the catalog")
	}
}

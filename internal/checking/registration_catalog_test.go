package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"
)

func TestRuleBehavioursMatchCatalog(t *testing.T) {
	t.Parallel()
	rules := ConfigurationRules()
	for _, native := range registration.All {
		if _, declared := rules.FindRule(native.Name); !declared {
			t.Errorf("native rule %q has no declaration", native.Name)
		}
		if registration.HasRule(native.Name) != native.Executable() {
			t.Errorf("native rule %q executes without an implementation entry", native.Name)
		}
	}
}

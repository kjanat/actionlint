package checking

import (
	"testing"

	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
)

func TestRuleBehavioursMatchCatalog(t *testing.T) {
	t.Parallel()
	rules := ConfigurationRules()
	registrations := ruleexec.Registrations(rules)
	if len(registrations) != len(rules.Rules()) {
		t.Fatal("registration count differs from the declared rule count")
	}
	for _, d := range registrations {
		if d.Build == nil && d.Script == nil && d.Enabled == nil {
			continue
		}
		if !ruleexec.HasRule(d.Name) {
			t.Errorf("descriptor %q carries implementation without a implementation entry", d.Name)
		}
	}
}

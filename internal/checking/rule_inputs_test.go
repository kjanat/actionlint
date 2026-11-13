package checking

import (
	"slices"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestRuleInputRequirementsReachConfigurationAndCatalogue(t *testing.T) {
	t.Parallel()
	listed, ok := FindRule("deprecated-commands")
	if !ok || !slices.Equal(listed.RequiredInputs, []rule.InputKind{rule.Scripts, rule.Invocations}) {
		t.Fatalf("catalogue requirements: %+v", listed)
	}
	configured, ok := ConfigurationRules().FindRule("deprecated-commands")
	if !ok || !slices.Equal(configured.RequiredInputs, listed.RequiredInputs) {
		t.Fatalf("configuration requirements: %+v", configured)
	}
	configured.RequiredInputs[0] = rule.Source
	listed.RequiredInputs[0] = rule.Dependencies
	again, _ := ConfigurationRules().FindRule("deprecated-commands")
	if !slices.Equal(again.RequiredInputs, []rule.InputKind{rule.Scripts, rule.Invocations}) {
		t.Fatalf("caller changed cached requirements: %+v", again.RequiredInputs)
	}
}

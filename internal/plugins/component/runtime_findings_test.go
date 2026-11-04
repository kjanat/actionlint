package component

import (
	"math"
	"testing"
)

func TestValidInputKindDiscriminant(t *testing.T) {
	if !validInputKindDiscriminant(uint32(len(inputKinds) - 1)) {
		t.Fatal("last known input kind discriminant rejected")
	}
	for _, discriminant := range []uint32{uint32(len(inputKinds)), math.MaxUint32} {
		if validInputKindDiscriminant(discriminant) {
			t.Fatalf("unknown input kind discriminant %d accepted", discriminant)
		}
	}
}

func TestValidateSuppressionRuleNameRejectsBuiltin(t *testing.T) {
	if err := validateSuppressionRuleName("disallow-suppressions"); err == nil {
		t.Fatal("built-in rule name accepted for plugin")
	}
	for _, name := range []string{"custom-plugin-rule", "shellcheck", "ruff"} {
		if err := validateSuppressionRuleName(name); err != nil {
			t.Errorf("rule %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "my rule", "my\trule", "my\u00a0rule", "my,rule", "my[rule", "my]rule"} {
		if err := validateSuppressionRuleName(name); err == nil {
			t.Errorf("rule %q cannot round-trip through suppression selectors", name)
		}
	}
}

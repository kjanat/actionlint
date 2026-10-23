package component

import (
	"math"
	"testing"
)

func TestValidFactDiscriminant(t *testing.T) {
	if !validFactDiscriminant(uint32(len(factNames) - 1)) {
		t.Fatal("last known fact discriminant rejected")
	}
	for _, discriminant := range []uint32{uint32(len(factNames)), math.MaxUint32} {
		if validFactDiscriminant(discriminant) {
			t.Fatalf("unknown fact discriminant %d accepted", discriminant)
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
}

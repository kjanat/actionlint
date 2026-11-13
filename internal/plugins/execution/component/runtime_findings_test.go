package component

import (
	"math"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestValidInputKindDiscriminant(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	reserved, err := rulemeta.NewNames([]rulemeta.Descriptor{{
		Name: "forbidden-suppression", Aliases: []rulemeta.Alias{{Name: "disallow-suppressions", Deprecated: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forbidden-suppression", "disallow-suppressions"} {
		if err := validateSuppressionRuleName(reserved, name); err == nil {
			t.Fatalf("reserved rule name %q accepted for plugin", name)
		}
	}
	for _, name := range []string{"custom-plugin-rule", "shellcheck", "ruff"} {
		if err := validateSuppressionRuleName(reserved, name); err != nil {
			t.Errorf("rule %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "my rule", "my\trule", "my\u00a0rule", "my,rule", "my[rule", "my]rule"} {
		if err := validateSuppressionRuleName(reserved, name); err == nil {
			t.Errorf("rule %q cannot round-trip through suppression selectors", name)
		}
	}
}

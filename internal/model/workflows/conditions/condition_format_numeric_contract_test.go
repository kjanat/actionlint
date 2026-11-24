package conditions_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"

	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
)

func TestConstantFormatUncertainArguments(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"format('{0}', -0)", "format('{0}', -0.0)",
		"format('{0}', fromJSON('-0'))", "format('{0}', 1e15)",
		"format('{0}', -1e15)", "format('{0}', 1e-5)",
		"format('{0}', 0.12345678901234567)", "format('{0}', fromJSON('1e309'))",
		"format('{0}', inputs.number)",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			contextValue, known := evaluate.ConditionConstantValue(syntax.ParseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", contextValue)
			}
			if _, known := conditions.Condition(value.Condition("${{ "+expression+" }}", value.Origin{})); known {
				t.Fatal("uncertain format argument made invocation known")
			}
		})
	}
}

// TestConstantFormatCompositeArguments checks against Runny at runner
// 1ba885c2065fd9be317808ab3fd406f392635cf7 in the expression package.
func TestConstantFormatCompositeArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, want string }{
		{"format('{0}', fromJSON('{}'))", "Object"},
		{"format('{0}', fromJSON('[]'))", "Array"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node := syntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if got, known := evaluate.ConditionConstantValue(node); !known || got != tc.want {
				t.Fatalf("got %v/%v, want %s", got, known, tc.want)
			}
			if enabled, known := conditions.Condition(value.Condition("${{ "+tc.expression+" }}", value.Origin{})); !known || !enabled {
				t.Fatal("nonempty formatted composite must be truthy")
			}
		})
	}
}

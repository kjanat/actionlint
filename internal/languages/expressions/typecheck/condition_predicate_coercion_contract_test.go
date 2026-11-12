package typecheck_test

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestConstantPredicateAmbiguousNumbersStayUnknown(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"contains(-0, '0')", "startsWith(-0.0, '-')",
		"contains(1e15, '1')", "contains(1e-5, '0')",
		"endsWith(0.12345678901234567, '7')", "contains(fromJSON('1e309'), 'Infinity')",
		"contains(inputs.number, '1')",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			value, known := expreval.ConditionConstantValue(exprsyntax.ParseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", value)
			}
		})
	}
}

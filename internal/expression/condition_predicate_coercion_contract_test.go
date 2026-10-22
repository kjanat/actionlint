package expression_test

import (
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

func TestConstantPredicateAmbiguousNumbersStayUnknown(t *testing.T) {
	for _, expression := range []string{
		"contains(-0, '0')", "startsWith(-0.0, '-')",
		"contains(1e15, '1')", "contains(1e-5, '0')",
		"endsWith(0.12345678901234567, '7')", "contains(fromJSON('1e309'), 'Infinity')",
		"contains(fromJSON('{}'), 'Object')", "startsWith(fromJSON('[]'), 'Array')",
		"contains(inputs.number, '1')",
	} {
		t.Run(expression, func(t *testing.T) {
			value, known := exprlang.ConditionConstantValue(exprlang.ParseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", value)
			}
		})
	}
}

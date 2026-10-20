package execution_test

import (
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
)

func TestConstantFormatUncertainArguments(t *testing.T) {
	for _, expression := range []string{
		"format('{0}', -0)", "format('{0}', -0.0)",
		"format('{0}', fromJSON('-0'))", "format('{0}', 1e15)",
		"format('{0}', -1e15)", "format('{0}', 1e-5)",
		"format('{0}', 0.12345678901234567)", "format('{0}', fromJSON('1e309'))",
		"format('{0}', fromJSON('{}'))", "format('{0}', fromJSON('[]'))",
		"format('{0}', inputs.number)",
	} {
		t.Run(expression, func(t *testing.T) {
			value, known := exprlang.ConditionConstantValue(exprlang.ParseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", value)
			}
			if _, known := execution.Condition(&workflowlang.String{Value: "${{ " + expression + " }}"}); known {
				t.Fatal("uncertain format argument made invocation known")
			}
		})
	}
}

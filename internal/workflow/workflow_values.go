package workflow

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
)

func WorkflowExpressionLiteral(s *String) (any, bool) {
	expr := exprlang.ParseAssignedExpression(s.Value)
	if expr == nil {
		return nil, false
	}
	return exprlang.ConditionConstantValue(expr)
}

// WorkflowScalarString applies the runner's scalar conversion for string schemas.
func WorkflowScalarString(value any) (string, bool) {
	return mapping.ScalarString(value)
}

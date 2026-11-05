package workflow

import (
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func WorkflowExpressionLiteral(s *String) (any, bool) {
	expr := exprsyntax.ParseAssignedExpression(s.Value)
	if expr == nil {
		return nil, false
	}
	return expreval.ConditionConstantValue(expr)
}

// WorkflowScalarString applies the runner's scalar conversion for string schemas.
func WorkflowScalarString(value any) (string, bool) {
	return mapping.ScalarString(value)
}

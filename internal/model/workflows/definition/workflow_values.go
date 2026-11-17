package definition

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func WorkflowExpressionLiteral(s *String) (any, bool) {
	expr := syntax.ParseAssignedExpression(s.Value)
	if expr == nil {
		return nil, false
	}
	return evaluate.ConditionConstantValue(expr)
}

// WorkflowScalarString applies the runner's scalar conversion for string schemas.
func WorkflowScalarString(value any) (string, bool) {
	return mappings.ScalarString(value)
}

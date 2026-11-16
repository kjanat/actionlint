// Package values evaluates known expression values in workflow fields.
package values

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Constant evaluates an assigned expression when all its required inputs are known.
func Constant(value *definition.String) (any, bool) {
	if value == nil {
		return nil, false
	}
	expr := syntax.ParseAssignedExpression(value.Value)
	if expr == nil {
		return nil, false
	}
	return evaluate.ConditionConstantValue(expr)
}

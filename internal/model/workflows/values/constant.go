// Package values evaluates known expression values in workflow fields.
package values

import (
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Constant evaluates an assigned expression when all its required inputs are known.
func Constant(value *definition.String) (any, bool) {
	if value == nil {
		return nil, false
	}
	expr := exprsyntax.ParseAssignedExpression(value.Value)
	if expr == nil {
		return nil, false
	}
	return expreval.ConditionConstantValue(expr)
}

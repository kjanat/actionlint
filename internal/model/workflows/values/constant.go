// Package values evaluates known expression values in workflow fields.
package values

import (
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Constant evaluates an assigned expression when all its required inputs are known.
func Constant(value declaration.StringView) (any, bool) {
	if !value.Present() {
		return nil, false
	}
	expr := exprsyntax.ParseAssignedExpression(value.Value())
	if expr == nil {
		return nil, false
	}
	return expreval.ConditionConstantValue(expr)
}

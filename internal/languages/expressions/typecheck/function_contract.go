package typecheck

import (
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func (sema *Checker) logicalRightDemand(node *exprsyntax.LogicalOpNode) exprfunctions.ArgumentDemand {
	value, found := sema.constants.Result(node.Left)
	if !found || value.State != exprvalues.EvaluationKnown {
		return exprfunctions.DemandConditional
	}
	if exprvalues.ExpressionTruthy(value.Value) == (node.Kind == exprsyntax.LogicalOpNodeKindOr) {
		return exprfunctions.DemandSkipped
	}
	return exprfunctions.DemandRequired
}

package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func (sema *Checker) logicalRightDemand(node *syntax.LogicalOpNode) functions.ArgumentDemand {
	value, found := sema.constants.Result(node.Left)
	if !found || value.State != exprvalues.EvaluationKnown {
		return functions.DemandConditional
	}
	if exprvalues.ExpressionTruthy(value.Value) == (node.Kind == syntax.LogicalOpNodeKindOr) {
		return functions.DemandSkipped
	}
	return functions.DemandRequired
}

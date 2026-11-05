package typecheck

import (
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func logicalRightDemand(node *exprsyntax.LogicalOpNode) exprfunctions.ArgumentDemand {
	value, known := expreval.ConditionConstantValue(node.Left)
	if !known {
		return exprfunctions.DemandConditional
	}
	if exprvalues.ExpressionTruthy(value) == (node.Kind == exprsyntax.LogicalOpNodeKindOr) {
		return exprfunctions.DemandSkipped
	}
	return exprfunctions.DemandRequired
}

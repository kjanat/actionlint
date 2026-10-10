package actionlint

// checkUnsoundTernaries uses the shared Actions evaluator to identify falsy middle
// operands. Unknown middle operands are left alone to allow conditional fallbacks.
func (rule *RuleExpression) checkUnsoundTernaries(expr ExprNode, line, col int) {
	VisitExprNode(expr, func(node, _ ExprNode, entering bool) {
		if !entering {
			return
		}
		or, ok := node.(*LogicalOpNode)
		if !ok || or.Kind != LogicalOpNodeKindOr {
			return
		}
		and, ok := or.Left.(*LogicalOpNode)
		if !ok || and.Kind != LogicalOpNodeKindAnd {
			return
		}
		value, known := conditionConstantValue(and.Right)
		if !known || expressionTruthy(value) {
			return
		}
		err := errorAtExpr(and.Right, "falsy middle operand in a && b || c always selects the fallback; this expression cannot select the middle value")
		err.rule = "unsound-ternary"
		rule.exprError(err, line, col)
	})
}

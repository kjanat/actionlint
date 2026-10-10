package actionlint

import "strings"

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
		truthy, known := ternaryOperandTruthy(and.Right)
		if !known || truthy {
			return
		}
		err := errorAtExpr(and.Right, "falsy middle operand in a && b || c always selects the fallback; this expression cannot select the middle value")
		err.rule = "unsound-ternary"
		rule.exprError(err, line, col)
	})
}

// ternaryOperandTruthy preserves definite truthiness when a logical expression
// can return several distinct values with the same truthiness.
func ternaryOperandTruthy(expr ExprNode) (bool, bool) {
	switch n := expr.(type) {
	case *NotOpNode:
		truthy, known := ternaryOperandTruthy(n.Operand)
		return !truthy, known
	case *LogicalOpNode:
		left, leftKnown := ternaryOperandTruthy(n.Left)
		shortCircuit := n.Kind == LogicalOpNodeKindOr
		if leftKnown && left == shortCircuit {
			return left, true
		}
		right, rightKnown := ternaryOperandTruthy(n.Right)
		if leftKnown || (rightKnown && right == shortCircuit) {
			return right, rightKnown
		}
		return false, false
	case *FuncCallNode:
		if strings.EqualFold(n.Callee, "case") {
			return ternaryCaseTruthy(n.Args)
		}
	}
	value, known := conditionConstantValue(expr)
	return expressionTruthy(value), known
}

func ternaryCaseTruthy(args []ExprNode) (bool, bool) {
	if len(args) < 3 || len(args)%2 == 0 {
		return false, false
	}
	var result, haveResult bool
	merge := func(value ExprNode) bool {
		truthy, known := ternaryOperandTruthy(value)
		if !known || haveResult && truthy != result {
			return false
		}
		result, haveResult = truthy, true
		return true
	}
	for i := 0; i < len(args)-1; i += 2 {
		truthy, known := ternaryOperandTruthy(args[i])
		if known && !truthy {
			continue
		}
		if !merge(args[i+1]) {
			return false, false
		}
		if known {
			return result, true
		}
	}
	if !merge(args[len(args)-1]) {
		return false, false
	}
	return result, true
}

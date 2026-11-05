package correctness

import (
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// UnsoundTernary uses the shared Actions evaluator to identify falsy middle
// operands. Unknown middle operands are left alone to allow conditional fallbacks.
func UnsoundTernary(expr exprsyntax.Node) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(expr, func(node, _ exprsyntax.Node, entering bool) {
		if !entering {
			return
		}
		or, ok := node.(*exprsyntax.LogicalOpNode)
		if !ok || or.Kind != exprsyntax.LogicalOpNodeKindOr {
			return
		}
		and, ok := or.Left.(*exprsyntax.LogicalOpNode)
		if !ok || and.Kind != exprsyntax.LogicalOpNodeKindAnd {
			return
		}
		truthy, known := ternaryOperandTruthy(and.Right)
		if !known || truthy {
			return
		}
		span := exprsyntax.ExpressionSpan(and.Right)
		err := &exprlang.Error{
			Rule: "unsound-ternary", Code: "falsy-middle-operand",
			Message: "falsy middle operand in a && b || c always selects the fallback; this expression cannot select the middle value",
			Offset:  span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column,
		}
		findings = append(findings, err)
	})
	return findings
}

// ternaryOperandTruthy preserves definite truthiness when a logical expression
// can return several distinct values with the same truthiness.
func ternaryOperandTruthy(expr exprsyntax.Node) (bool, bool) {
	switch n := expr.(type) {
	case *exprsyntax.NotOpNode:
		truthy, known := ternaryOperandTruthy(n.Operand)
		return !truthy, known
	case *exprsyntax.LogicalOpNode:
		left, leftKnown := ternaryOperandTruthy(n.Left)
		shortCircuit := n.Kind == exprsyntax.LogicalOpNodeKindOr
		if leftKnown && left == shortCircuit {
			return left, true
		}
		right, rightKnown := ternaryOperandTruthy(n.Right)
		if leftKnown || (rightKnown && right == shortCircuit) {
			return right, rightKnown
		}
		return false, false
	case *exprsyntax.FuncCallNode:
		if strings.EqualFold(n.Callee, "case") {
			return ternaryCaseTruthy(n.Args)
		}
	}
	value, known := expreval.ConditionConstantValue(expr)
	return exprvalues.ExpressionTruthy(value), known
}

func ternaryCaseTruthy(args []exprsyntax.Node) (bool, bool) {
	if len(args) < 3 || len(args)%2 == 0 {
		return false, false
	}
	var result, haveResult bool
	merge := func(value exprsyntax.Node) bool {
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

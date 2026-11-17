package correctness

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// UnsoundTernaryRule is the single definition of the unsound-ternary rule.
var UnsoundTernaryRule = &define.Rule{
	Name:        "unsound-ternary",
	Description: "Report a && b || c expressions where b is always falsy",
	Category:    "correctness",
	Subcategory: "expressions",
	Recommended: true,
	Help:        "The middle operand of a && b || c must be truthy to select b; use an explicit conditional or a truthy representation.",
	DeclaredBy:  "expression",
}

// UnsoundTernary uses the shared Actions evaluator to identify falsy middle
// operands. Unknown middle operands are left alone to allow conditional fallbacks.
func UnsoundTernary(expr syntax.Node) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(expr, func(node, _ syntax.Node, entering bool) {
		if !entering {
			return
		}
		or, ok := node.(*syntax.LogicalOpNode)
		if !ok || or.Kind != syntax.LogicalOpNodeKindOr {
			return
		}
		and, ok := or.Left.(*syntax.LogicalOpNode)
		if !ok || and.Kind != syntax.LogicalOpNodeKindAnd {
			return
		}
		truthy, known := ternaryOperandTruthy(and.Right)
		if !known || truthy {
			return
		}
		span := syntax.ExpressionSpan(and.Right)
		err := &typecheck.Error{
			Rule: "unsound-ternary", Code: "falsy-middle-operand",
			Message: "falsy middle operand in a && b || c always selects the fallback; this expression cannot select the middle value",
			Offset:  span.Start, EndOffset: span.End, Position: span.Position,
		}
		findings = append(findings, err)
	})
	return findings
}

// ternaryOperandTruthy preserves definite truthiness when a logical expression
// can return several distinct values with the same truthiness.
func ternaryOperandTruthy(expr syntax.Node) (bool, bool) {
	switch n := expr.(type) {
	case *syntax.NotOpNode:
		truthy, known := ternaryOperandTruthy(n.Operand)
		return !truthy, known
	case *syntax.LogicalOpNode:
		left, leftKnown := ternaryOperandTruthy(n.Left)
		shortCircuit := n.Kind == syntax.LogicalOpNodeKindOr
		if leftKnown && left == shortCircuit {
			return left, true
		}
		right, rightKnown := ternaryOperandTruthy(n.Right)
		if leftKnown || (rightKnown && right == shortCircuit) {
			return right, rightKnown
		}
		return false, false
	case *syntax.FuncCallNode:
		if strings.EqualFold(n.Callee, "case") {
			return ternaryCaseTruthy(n.Args)
		}
	}
	value, known := evaluate.ConditionConstantValue(expr)
	return values.ExpressionTruthy(value), known
}

func ternaryCaseTruthy(args []syntax.Node) (bool, bool) {
	if len(args) < 3 || len(args)%2 == 0 {
		return false, false
	}
	var result, haveResult bool
	merge := func(value syntax.Node) bool {
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

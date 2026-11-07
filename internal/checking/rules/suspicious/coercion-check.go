package suspicious

import (
	"fmt"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// SuspiciousCoercion reports accepted conversions that can hide mistaken operand types.
// Calls use the checker's selected signature and demand; this rule does not evaluate them.
func SuspiciousCoercion(root exprsyntax.Node, types exprlang.InferredTypes, calls exprlang.CheckedCalls) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		if entering {
			return
		}
		switch node := node.(type) {
		case *exprsyntax.CompareOpNode:
			left, leftKnown := types.TypeOf(node.Left)
			right, rightKnown := types.TypeOf(node.Right)
			if leftKnown && rightKnown && suspiciousComparison(node.Kind, left, right) {
				findings = append(findings, expressionFinding(node, "coercion", fmt.Sprintf("comparison of %s and %s with %s uses Actions coercion or NaN; check whether these operand types were intended", left, right, node.Kind)))
			}
		case *exprsyntax.FuncCallNode:
			facts, known := calls.CallOf(node)
			if !known || !facts.SignatureMatched || facts.Demand == exprfunctions.DemandSkipped {
				return
			}
			for index, arg := range facts.Arguments {
				if arg.Demand == exprfunctions.DemandSkipped || arg.Conversion != exprfunctions.ConversionString || (arg.Source.Kind() != exprtypes.KindBool && arg.Source.Kind() != exprtypes.KindNull) {
					continue
				}
				operation := "converts"
				if facts.Demand == exprfunctions.DemandConditional || arg.Demand == exprfunctions.DemandConditional {
					operation = "may convert"
				}
				findings = append(findings, expressionFinding(node.Args[index], "coercion", fmt.Sprintf("%s argument of %s() %s %s to a string; check whether a string value was intended", messages.Ordinal(index+1), node.Callee, operation, arg.Source)))
			}
		}
	})
	return findings
}

// suspiciousComparison identifies supported comparisons whose conversion can hide
// a mistaken operand. Unknown types provide no evidence for this warning.
func suspiciousComparison(op exprsyntax.CompareOpNodeKind, left, right exprtypes.Type) bool {
	l, r := left.Kind(), right.Kind()
	if l == exprtypes.KindAny || r == exprtypes.KindAny {
		return false
	}
	composite := func(k exprtypes.Kind) bool { return k == exprtypes.KindArray || k == exprtypes.KindObject }
	if op.IsEqualityOp() {
		return l != r && l != exprtypes.KindNull && r != exprtypes.KindNull && (composite(l) || composite(r))
	}
	return composite(l) || composite(r) || l == exprtypes.KindNull || r == exprtypes.KindNull || l == exprtypes.KindBool || r == exprtypes.KindBool
}

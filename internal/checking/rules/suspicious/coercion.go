package suspicious

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var CoercionRule = &define.Rule{
	Name:            "coercion",
	Aliases:         []rule.Alias{{Name: "suspicious-coercion", Deprecated: true}},
	Description:     "Warn about surprising string and comparison coercions",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Help:            "Use a string literal for text, or compare values of the intended types.",
	Examples:        []rule.Example{rule.WorkflowExample("Test the boolean directly.", "    if: startsWith(github.ref_protected, 'true')\n", "    if: github.ref_protected\n")},
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	DefaultLevel:    "warn",
	DeclaredBy:      "expression",
}

// SuspiciousCoercion takes call signatures from the checker and evaluates no call.
func SuspiciousCoercion(root syntax.Node, types typecheck.InferredTypes, calls typecheck.CheckedCalls) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(root, func(node, _ syntax.Node, entering bool) {
		if entering {
			return
		}
		switch node := node.(type) {
		case *syntax.CompareOpNode:
			left, leftKnown := types.TypeOf(node.Left)
			right, rightKnown := types.TypeOf(node.Right)
			if leftKnown && rightKnown && suspiciousComparison(node.Kind, left, right) {
				findings = append(findings, expressionFinding(node, "coercion", fmt.Sprintf("comparison of %s and %s with %s uses Actions coercion or NaN; check whether these operand types were intended", left, right, node.Kind)))
			}
		case *syntax.FuncCallNode:
			facts, known := calls.CallOf(node)
			if !known || !facts.SignatureMatched || facts.Demand == functions.DemandSkipped {
				return
			}
			for index, arg := range facts.Arguments {
				if arg.Demand == functions.DemandSkipped || arg.Conversion != functions.ConversionString || (arg.Source.Kind() != exprtypes.KindBool && arg.Source.Kind() != exprtypes.KindNull) {
					continue
				}
				operation := "converts"
				if facts.Demand == functions.DemandConditional || arg.Demand == functions.DemandConditional {
					operation = "may convert"
				}
				findings = append(findings, expressionFinding(node.Args[index], "coercion", fmt.Sprintf("%s argument of %s() %s %s to a string; check whether a string value was intended", messages.Ordinal(index+1), node.Callee, operation, arg.Source)))
			}
		}
	})
	return findings
}

func suspiciousComparison(op syntax.CompareOpNodeKind, left, right exprtypes.Type) bool {
	l, r := left.Kind(), right.Kind()
	if l == exprtypes.KindAny || r == exprtypes.KindAny {
		return false
	}
	composite := func(k exprtypes.TypeKind) bool { return k == exprtypes.KindArray || k == exprtypes.KindObject }
	if op.IsEqualityOp() {
		return l != r && l != exprtypes.KindNull && r != exprtypes.KindNull && (composite(l) || composite(r))
	}
	return composite(l) || composite(r) || l == exprtypes.KindNull || r == exprtypes.KindNull || l == exprtypes.KindBool || r == exprtypes.KindBool
}

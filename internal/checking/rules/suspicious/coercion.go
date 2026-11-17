package suspicious

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

// CoercionRule is the single definition of the coercion rule.
var CoercionRule = &define.Rule{
	Name:            "coercion",
	Aliases:         []rule.Alias{{Name: "suspicious-coercion", Deprecated: true}},
	Description:     "Warn about surprising string and comparison coercions",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Explanation:     "Warn when a string parameter converts a boolean or null, when a comparison mixes a collection with a type other than null, or when an ordering comparison uses booleans, null or collections. These expressions are valid in GitHub Actions. Unknown types do not establish a suspicious conversion. Collection equality within the same type is allowed because Actions compares instance identity. Equality with null is a presence check and is allowed.",
	Help:            "Check the intended argument or operand type. Use a string literal when text is intended, or compare values of the intended types. This rule offers no automatic edit because the intended value is unknown.",
	Examples:        []rule.Example{rule.WorkflowExample("Use the boolean value directly when checking branch protection.", "    if: startsWith(github.ref_protected, 'true')\n", "    if: github.ref_protected\n")},
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	DefaultLevel:    "warn",
	DeclaredBy:      "expression",
}

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
	composite := func(k exprtypes.TypeKind) bool { return k == exprtypes.KindArray || k == exprtypes.KindObject }
	if op.IsEqualityOp() {
		return l != r && l != exprtypes.KindNull && r != exprtypes.KindNull && (composite(l) || composite(r))
	}
	return composite(l) || composite(r) || l == exprtypes.KindNull || r == exprtypes.KindNull || l == exprtypes.KindBool || r == exprtypes.KindBool
}

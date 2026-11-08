package suspicious

import (
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

// JoinLiteralDeclaration describes the optional check for redundant join calls.
func JoinLiteralDeclaration() rule.Declaration {
	return rule.Declaration{
		Name:            "join-literal",
		Description:     "Report redundant join calls on string literals",
		Category:        "suspicious",
		Subcategory:     "expressions",
		Maturity:        "nursery",
		Provider:        "actionlint",
		Recommended:     false,
		DefaultLevel:    "info",
		SupportsActions: true,
		Scope:           rule.CompositeConfiguration,
		Explanation: "join() accepts a string and returns it unchanged. " +
			"Context names inside the quotes are literal text and are not evaluated. " +
			"The separator has no effect on that string. " +
			"This optional check reports a string literal as the first argument even when the call may be deliberate. " +
			"Arrays and strings supplied by other expressions are not reported.",
		Help: "If you meant to use literal text, remove the redundant call. " +
			"To combine expression values, use format() or pass an array to join(). " +
			"This rule does not offer an automatic edit.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#join-literal",
	}
}

// JoinLiteral reports join calls with a string literal as the first argument.
func JoinLiteral(root exprsyntax.Node, calls exprlang.CheckedCalls) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		call, ok := node.(*exprsyntax.FuncCallNode)
		if !entering || !ok || !strings.EqualFold(call.Callee, "join") || len(call.Args) == 0 {
			return
		}
		if _, literal := call.Args[0].(*exprsyntax.StringNode); !literal {
			return
		}
		facts, known := calls.CallOf(call)
		if !known || !facts.SignatureMatched || facts.Demand == exprfunctions.DemandSkipped {
			return
		}
		findings = append(findings, expressionFinding(
			call.Args[0],
			"join-literal",
			"join() returns this string literal unchanged; quoted expressions are not evaluated "+
				"and the separator does not combine its contents",
		))
	})
	return findings
}

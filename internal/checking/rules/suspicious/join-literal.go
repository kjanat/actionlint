package suspicious

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// JoinLiteralRule is the single definition of the join-literal rule.
var JoinLiteralRule = &define.Rule{
	Name:            "join-literal",
	Description:     "Report redundant join calls on string literals",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Maturity:        "nursery",
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
	DeclaredBy: "expression",
}

// JoinLiteral reports join calls with a string literal as the first argument.
func JoinLiteral(root syntax.Node, calls typecheck.CheckedCalls) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(root, func(node, _ syntax.Node, entering bool) {
		call, ok := node.(*syntax.FuncCallNode)
		if !entering || !ok || !strings.EqualFold(call.Callee, "join") || len(call.Args) == 0 {
			return
		}
		if _, literal := call.Args[0].(*syntax.StringNode); !literal {
			return
		}
		facts, known := calls.CallOf(call)
		if !known || !facts.SignatureMatched || facts.Demand == functions.DemandSkipped {
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

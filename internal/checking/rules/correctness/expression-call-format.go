package correctness

import (
	"fmt"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// UnusedFormatArguments reports arguments absent from a known format template.
// Invalid placeholders are evaluation failures, reported separately by the language checker.
func UnusedFormatArguments(root exprsyntax.Node, calls exprlang.CheckedCalls) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		call, ok := node.(*exprsyntax.FuncCallNode)
		if entering || !ok || !strings.EqualFold(call.Callee, "format") {
			return
		}
		facts, known := calls.CallOf(call)
		if !known || !facts.SignatureMatched || facts.Demand == exprfunctions.DemandSkipped {
			return
		}
		first := facts.Arguments[0].Value
		format, isString := first.Value.(string)
		if first.State != exprvalues.EvaluationKnown || !isString {
			return
		}
		count := len(facts.Arguments) - 1
		plan := exprfunctions.PlanFormat(format, count)
		for index := range count {
			if plan.References(index) {
				continue
			}
			token := call.Token()
			findings = append(findings, &exprlang.Error{Rule: "expression-call", Code: "format-placeholder", Message: fmt.Sprintf("format string %q does not contain placeholder {%d}. remove argument which is unused in the format string", format, index), Offset: token.Offset, EndOffset: token.Offset + len(token.Value), Line: token.Line, Column: token.Column, Advisory: true})
		}
	})
	return findings
}

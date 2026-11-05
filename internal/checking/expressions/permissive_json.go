package expressions

import (
	"encoding/json"
	"errors"
	"strings"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// permissiveJSONCalls locates known fromJSON arguments accepted only by the
// runner's loose reader. It does not change semantic validity or inferred types.
func permissiveJSONCalls(expr exprsyntax.Node) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(expr, func(node, _ exprsyntax.Node, entering bool) {
		call, ok := node.(*exprsyntax.FuncCallNode)
		if !entering || !ok || !strings.EqualFold(call.Callee, "fromJSON") || len(call.Args) != 1 {
			return
		}
		value, known := expreval.ConditionConstantValue(call.Args[0])
		text, isString := value.(string)
		if !known || !isString || json.Valid([]byte(text)) {
			return
		}
		if _, _, err := runnerjson.Read(text); err == nil || errors.Is(err, runnerjson.ErrUnsupportedValue) {
			span := exprsyntax.ExpressionSpan(call.Args[0])
			findings = append(findings, &exprlang.Error{
				Rule: "expression-call", Code: "permissive-json", Message: "you naughty boy, I see you",
				Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column,
			})
		}
	})
	return findings
}

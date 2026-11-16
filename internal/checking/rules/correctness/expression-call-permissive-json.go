package correctness

import (
	"encoding/json"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// PermissiveJSONCalls locates known fromJSON arguments accepted only by the
// runner's loose reader. It does not change semantic validity or inferred types.
func PermissiveJSONCalls(expr syntax.Node) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(expr, func(node, _ syntax.Node, entering bool) {
		call, ok := node.(*syntax.FuncCallNode)
		if !entering || !ok || !strings.EqualFold(call.Callee, "fromJSON") || len(call.Args) != 1 {
			return
		}
		value, known := evaluate.ConditionConstantValue(call.Args[0])
		text, isString := value.(string)
		if !known || !isString || json.Valid([]byte(text)) {
			return
		}
		if _, _, err := runnerjson.Read(text); err == nil || errors.Is(err, runnerjson.ErrUnsupportedValue) {
			span := syntax.ExpressionSpan(call.Args[0])
			findings = append(findings, &typecheck.Error{
				Rule: "expression-call", Code: "permissive-json", Message: "you naughty boy, I see you",
				Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column,
			})
		}
	})
	return findings
}

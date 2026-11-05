package typecheck

import (
	"encoding/json"
	"errors"
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

// PermissiveJSONCalls locates known fromJSON arguments accepted only by the
// runner's loose reader. It does not change semantic validity or inferred types.
func PermissiveJSONCalls(expr exprsyntax.Node) []*Error {
	var findings []*Error
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
			findings = append(findings, errorAtExpr(permissiveJSON, call.Args[0], "you naughty boy, I see you"))
		}
	})
	return findings
}

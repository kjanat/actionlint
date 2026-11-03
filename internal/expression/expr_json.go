package expression

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// PermissiveJSONCalls locates known fromJSON arguments accepted only by the
// runner's loose reader. It does not change semantic validity or inferred types.
func PermissiveJSONCalls(expr Node) []*Error {
	var findings []*Error
	Visit(expr, func(node, _ Node, entering bool) {
		call, ok := node.(*FuncCallNode)
		if !entering || !ok || !strings.EqualFold(call.Callee, "fromJSON") || len(call.Args) != 1 {
			return
		}
		value, known := ConditionConstantValue(call.Args[0])
		text, isString := value.(string)
		if !known || !isString || json.Valid([]byte(text)) {
			return
		}
		if _, _, err := readFromJSON(text); err == nil || errors.Is(err, errFromJSONUnsupported) {
			findings = append(findings, errorAtExpr(permissiveJSON, call.Args[0], "you naughty boy, I see you"))
		}
	})
	return findings
}

type jsonMemberCollision struct {
	First, Second string
	Offset        int64
}

// OrdinalIgnoreCase does not equate non-ASCII characters with ASCII letters,
// even when Unicode uppercasing maps dotless i or long s to I or S.
func ordinalIgnoreCaseKey(s string) string {
	return strings.Map(func(r rune) rune {
		upper := unicode.ToUpper(r)
		if r >= 128 && upper < 128 {
			return r
		}
		return upper
	}, s)
}

// JSONMemberCollisions finds case-insensitive duplicate keys in the first value
// read by fromJSON. Invalid input is diagnosed by the shared reader instead.
func JSONMemberCollisions(source string) []jsonMemberCollision {
	_, collisions, err := readFromJSON(source)
	if err != nil && !errors.Is(err, errFromJSONUnsupported) {
		return nil
	}
	return collisions
}

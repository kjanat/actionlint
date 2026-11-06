package completion

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprcheck "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// Item describes a property replacement in expression source byte offsets.
type Item struct {
	Label      string
	Detail     string
	Start, End int
}

var (
	completionAccess = regexp.MustCompile(`(?i)([a-z_][a-z0-9_-]*(?:(?:\s*\.\s*(?:[a-z_][a-z0-9_-]*|\*))|(?:\s*\[\s*(?:[0-9]+|'(?:[^']|'')*')\s*\]))*)\s*(?:\.\s*([a-z0-9_-]*)|\[\s*'([^']*))$`)
	completionRoot   = regexp.MustCompile(`(?i)[a-z_][a-z0-9_-]*$`)
)

// completeExpression suggests names from the checker's scope for unfinished dot/bracket
// access. The offset and returned edit positions count UTF-8 bytes.
func completeExpression(sema *exprcheck.Checker, source string, offset int) []Item {
	if offset < 0 || offset > len(source) {
		return nil
	}
	before := source[:offset]
	start := offset
	prefix := ""
	bracket := false
	var properties map[string]exprtypes.Type
	if match := completionAccess.FindStringSubmatchIndex(before); match != nil {
		// Do not complete text inside an expression string literal.
		if inExpressionString(before[:match[0]]) {
			return nil
		}
		receiver := before[match[2]:match[3]]
		at := match[4]
		if at < 0 {
			at = match[6]
			bracket = true
		}
		start = at
		prefix = before[at:]
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(receiver + "}}"))
		if err != nil {
			return nil
		}
		ty, errs := sema.Check(expreval.EvaluateSubexpressions(node))
		if len(errs) != 0 {
			return nil
		}
		if array, ok := ty.Array(); ok && array.Deref() {
			ty = array.Elem()
		}
		if object, ok := ty.Object(); ok {
			properties = maps.Collect(object.Properties())
		}
	} else {
		match := completionRoot.FindStringIndex(before)
		if match != nil {
			start = match[0]
			prefix = before[start:]
		}
		if inExpressionString(before[:start]) {
			return nil
		}
		// A complete receiver or unsupported index does not request root names.
		prior := strings.TrimSpace(before[:start])
		if prior != "" && !strings.ContainsAny(prior[len(prior)-1:], "(&|!,<>=") {
			return nil
		}
		properties = sema.AvailableContexts()
	}
	end := offset
	for end < len(source) && identifierByte(source[end]) {
		end++
	}
	result := []Item{}
	for _, name := range slices.Sorted(maps.Keys(properties)) {
		if !bracket && completionRoot.FindString(name) != name {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		result = append(result, Item{Label: name, Detail: typeLabel(properties[name]), Start: start, End: end})
	}
	return result
}

func inExpressionString(s string) bool {
	quoted := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			if quoted && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			quoted = !quoted
		}
	}
	return quoted
}

func identifierByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}

func typeLabel(t exprtypes.Type) string {
	if t.Kind() == exprtypes.KindObject {
		return "object"
	}
	if array, ok := t.Array(); ok {
		return "array<" + typeLabel(array.Elem()) + ">"
	}
	return t.String()
}

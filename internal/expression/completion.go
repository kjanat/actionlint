package expression

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Completion describes a property replacement in expression source byte offsets.
type Completion struct {
	Label      string
	Detail     string
	Start, End int
}

var (
	completionAccess = regexp.MustCompile(`(?i)([a-z_][a-z0-9_-]*(?:(?:\s*\.\s*(?:[a-z_][a-z0-9_-]*|\*))|(?:\s*\[\s*(?:[0-9]+|'(?:[^']|'')*')\s*\]))*)\s*(?:\.\s*([a-z0-9_-]*)|\[\s*'([^']*))$`)
	completionRoot   = regexp.MustCompile(`(?i)[a-z_][a-z0-9_-]*$`)
)

// Complete uses the same checker scope and property types as diagnostics. It
// accepts unfinished dot/bracket access and retains UTF-8 byte edit positions.
func (sema *Checker) Complete(source string, offset int) []Completion {
	if offset < 0 || offset > len(source) {
		return nil
	}
	before := source[:offset]
	start := offset
	prefix := ""
	bracket := false
	var properties map[string]Type
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
		node, err := NewParser().Parse(NewLexer(receiver + "}}"))
		if err != nil {
			return nil
		}
		ty, errs := sema.Check(node)
		if len(errs) != 0 {
			return nil
		}
		if array, ok := ty.(*ArrayType); ok && array.Deref {
			ty = array.Elem
		}
		if object, ok := ty.(*ObjectType); ok {
			properties = object.Props
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
		properties = map[string]Type{}
		for _, name := range sema.availableContexts {
			if ty, ok := sema.vars[name]; ok {
				properties[name] = ty
			}
		}
	}
	end := offset
	for end < len(source) && identifierByte(source[end]) {
		end++
	}
	result := []Completion{}
	for _, name := range slices.Sorted(maps.Keys(properties)) {
		if !bracket && completionRoot.FindString(name) != name {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		result = append(result, Completion{Label: name, Detail: typeLabel(properties[name]), Start: start, End: end})
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

func typeLabel(t Type) string {
	switch t := t.(type) {
	case *ObjectType:
		return "object"
	case *ArrayType:
		return "array<" + typeLabel(t.Elem) + ">"
	default:
		return t.String()
	}
}

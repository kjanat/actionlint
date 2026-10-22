package expression

import (
	"strings"
	"unicode/utf8"
)

// ConditionSource models the template reader's single string literal fold,
// followed by the workflow converter's empty-condition default. Computed
// strings remain expressions; their values are never reparsed as source.
func ConditionSource(source string) string {
	if literal := LiteralExpressionValue(source); literal != nil {
		source = *literal
	}
	if strings.TrimSpace(source) == "" {
		return "success()"
	}
	return source
}

// ParseConditionExpression folds only the original template, never text produced
// by that fold. The offset refers to the original expression when possible.
// A nil expression and error indicate a template with surrounding text.
func ParseConditionExpression(source string) (Node, *Error, int) {
	if LiteralExpressionValue(source) != nil || !ContainsExpression(source) {
		folded := ConditionSource(source) + "}}"
		lex := NewLexer(folded)
		expr, err := NewParser().Parse(lex)
		if err != nil && err.EndOffset > len(folded)-2 {
			// The closing delimiter is synthetic for a bare condition.
			err.EndOffset = len(folded) - 2
			if err.Offset > err.EndOffset {
				body := folded[:err.EndOffset]
				err.Offset = err.EndOffset
				err.Line = strings.Count(body, "\n") + 1
				err.Column = utf8.RuneCountInString(body[strings.LastIndexByte(body, '\n')+1:]) + 1
			}
		}
		if err == nil && lex.Offset() != len(folded) {
			lex.error("unexpected trailing text in condition")
			return nil, lex.Err(), 0
		}
		return expr, err, 0
	}
	if !strings.HasPrefix(source, "${{") {
		return nil, nil, 0
	}
	lex := NewLexer(source[3:])
	expr, err := NewParser().Parse(lex)
	if err == nil && lex.Offset() != len(source)-3 {
		return nil, nil, 0
	}
	return expr, err, 3
}

// ContainsExpression checks if the given string contains a ${{ }} placeholder or not. This function
// is identical to String.ContainsExpression method except for taking a standard string value.
func ContainsExpression(s string) bool {
	i := strings.Index(s, "${{")
	return i >= 0 && i < strings.Index(s, "}}")
}

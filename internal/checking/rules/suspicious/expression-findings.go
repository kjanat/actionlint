package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func expressionFinding(node syntax.Node, name, message string) *typecheck.Error {
	span := syntax.ExpressionSpan(node)
	return &typecheck.Error{Rule: name, Code: name, Message: message, Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column, Advisory: true}
}

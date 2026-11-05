package suspicious

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func expressionFinding(node exprsyntax.Node, name, message string) *exprlang.Error {
	span := exprsyntax.ExpressionSpan(node)
	return &exprlang.Error{Rule: name, Code: name, Message: message, Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column, Advisory: true}
}

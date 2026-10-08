package actionlint

import "strings"

// conditionSource models the template reader's single string literal fold,
// followed by the workflow converter's empty-condition default. Computed
// strings remain expressions; their values are never reparsed as source.
func conditionSource(source string) string {
	if literal := literalExpressionValue(source); literal != nil {
		source = *literal
	}
	if strings.TrimSpace(source) == "" {
		return "success()"
	}
	return source
}

// parseConditionExpression folds only the original template, never text produced
// by that fold. The offset refers to the original expression when possible.
// A nil expression and error indicate a template with surrounding text.
func parseConditionExpression(source string) (ExprNode, *ExprError, int) {
	if literalExpressionValue(source) != nil || !ContainsExpression(source) {
		expr, err := NewExprParser().Parse(NewExprLexer(conditionSource(source) + "}}"))
		return expr, err, 0
	}
	if !strings.HasPrefix(source, "${{") {
		return nil, nil, 0
	}
	lex := NewExprLexer(source[3:])
	expr, err := NewExprParser().Parse(lex)
	if err == nil && lex.Offset() != len(source)-3 {
		return nil, nil, 0
	}
	return expr, err, 3
}

package expressions

import (
	"strings"
	"unicode/utf8"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func (rule *Checker) scopeExpressionSource(value string, pos *workflowlang.Pos) func() {
	previous, offset := rule.expressionSource, rule.expressionOffset
	rule.expressionSource, rule.expressionOffset = nil, 0
	if pos != nil {
		var source *workflowlang.ScriptSource
		if rule.workflow.Present() {
			source = rule.workflow.ExpressionSources[*pos]
		}
		if override, exists := rule.scriptSources[*pos]; exists {
			source = override
		}
		if source != nil && source.Value == value {
			rule.expressionSource = source
		}
	}
	return func() { rule.expressionSource, rule.expressionOffset = previous, offset }
}

func (rule *Checker) expressionPosition(offset int, end bool) *workflowlang.Pos {
	source := rule.expressionSource
	if source == nil || offset < 0 || offset > len(source.Value) {
		return nil
	}
	prefix := source.Value[:offset]
	line, column := strings.Count(prefix, "\n")+1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:])+1
	if end || offset == len(source.Value) {
		position, _ := source.EndPos(line, column)
		return position
	}
	position, _ := source.Position(line, column)
	return position
}

// scalarPosition returns the position of an optional scalar.
func scalarPosition(s *workflowlang.String) *workflowlang.Pos {
	if s == nil {
		return nil
	}
	return s.Pos
}

func expressionLiteral(text string) (any, bool) {
	expression := exprsyntax.ParseAssignedExpression(text)
	if expression == nil {
		return nil, false
	}
	return expreval.ConditionConstantValue(expression)
}

func rawValuePosition(value workflowlang.RawYAMLValue) *workflowlang.Pos {
	if value == nil {
		return nil
	}
	return value.Pos()
}

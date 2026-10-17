package expressions

import (
	"strings"
	"unicode/utf8"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func (rule *Checker) scopeExpressionSource(value string, pos *workflowlang.Pos) func() {
	previous, offset := rule.expressionSource, rule.expressionOffset
	rule.expressionSource, rule.expressionOffset = nil, 0
	if rule.workflow != nil && pos != nil {
		if source := rule.workflow.ExpressionSources[*pos]; source != nil && source.Value == value {
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

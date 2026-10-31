package expressions

import (
	"strings"
	"unicode/utf8"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func (rule *Checker) scopeExpressionSource(value string, pos *workflowlang.Pos) func() {
	previous, offset := rule.expressionSource, rule.expressionOffset
	rule.expressionSource, rule.expressionOffset = workflowdecl.ScriptSourceView{}, 0
	if pos != nil {
		source, _ := rule.workflow.ExpressionSourcesAt(*pos)
		if override, exists := rule.scriptSources[*pos]; exists {
			source = override
		}
		if source.Present() && source.Value() == value {
			rule.expressionSource = source
		}
	}
	return func() { rule.expressionSource, rule.expressionOffset = previous, offset }
}

func (rule *Checker) expressionPosition(offset int, end bool) *workflowlang.Pos {
	source := rule.expressionSource
	if !source.Present() || offset < 0 || offset > len(source.Value()) {
		return nil
	}
	prefix := source.Value()[:offset]
	line, column := strings.Count(prefix, "\n")+1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:])+1
	if end || offset == len(source.Value()) {
		position, present := source.EndPos(line, column)
		if present {
			return &position
		}
		return nil
	}
	position, present := source.Position(line, column)
	if present {
		return &position
	}
	return nil
}

func viewPosition(view interface {
	Pos() (workflowlang.Pos, bool)
},
) *workflowlang.Pos {
	position, present := view.Pos()
	if !present {
		return nil
	}
	return &position
}

func expressionLiteral(text string) (any, bool) {
	expression := exprlang.ParseAssignedExpression(text)
	if expression == nil {
		return nil, false
	}
	return exprlang.ConditionConstantValue(expression)
}

func rawPosition(view interface {
	Position() (workflowlang.Pos, bool)
},
) *workflowlang.Pos {
	position, present := view.Position()
	if !present {
		return nil
	}
	return &position
}

func rawValuePosition(view workflowdecl.RawYAMLValueView) *workflowlang.Pos {
	if object, present := view.RawYAMLObject(); present {
		return rawPosition(object)
	}
	if array, present := view.RawYAMLArray(); present {
		return rawPosition(array)
	}
	if scalar, present := view.RawYAMLString(); present {
		return rawPosition(scalar)
	}
	return nil
}

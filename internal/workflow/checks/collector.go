package checks

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/workflow"
)

type findingCollector struct {
	name string
	out  []Finding
	step declaration.StepView
}

func (c *findingCollector) Error(p *workflow.Pos, text string) {
	c.out = append(c.out, Finding{Rule: c.name, Message: text, Start: position(p), Step: c.step})
}

func (c *findingCollector) Errorf(p *workflow.Pos, format string, args ...any) {
	c.Error(p, fmt.Sprintf(format, args...))
}

func viewPosition(pos workflow.Pos, present bool) *workflow.Pos {
	if !present {
		return nil
	}
	return &pos
}
func expressionLiteral(value declaration.StringView) (any, bool) {
	if !value.Present() {
		return nil, false
	}
	expr := expression.ParseAssignedExpression(value.Value())
	if expr == nil {
		return nil, false
	}
	return expression.ConditionConstantValue(expr)
}

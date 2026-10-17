package checks

import (
	"fmt"

	"actionlint.kjanat.dev/internal/workflow"
)

type findingCollector struct {
	name string
	out  []Finding
	step *workflow.Step
}

func (c *findingCollector) Error(p *workflow.Pos, text string) {
	c.out = append(c.out, Finding{Rule: c.name, Message: text, Start: position(p), Step: c.step})
}
func (c *findingCollector) Errorf(p *workflow.Pos, format string, args ...any) {
	c.Error(p, fmt.Sprintf(format, args...))
}

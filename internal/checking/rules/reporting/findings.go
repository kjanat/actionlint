// Package reporting collects rule findings and assigns workflow source positions.
package reporting

import (
	"fmt"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflow "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Finding associates a diagnostic with its owning step.
type Finding struct {
	findingmodel.Finding
	Step declaration.Step
}

// Collector attaches a rule name and step to each reported message.
type Collector struct {
	Rule     string
	Findings []Finding
	Step     declaration.Step
}

func (c *Collector) Error(p *sourcepos.Position, text string) {
	c.Findings = append(c.Findings, Finding{Rule: c.Rule, Message: text, Start: sourcepos.ZeroIfNil(p), Step: c.Step})
}

func (c *Collector) Errorf(p *sourcepos.Position, format string, args ...any) {
	c.Error(p, fmt.Sprintf(format, args...))
}

// Warning constructs a warning at the supplied source position.
func Warning(rule, code string, p *sourcepos.Position, format string, args ...any) Finding {
	return Finding{Rule: rule, Code: code, Severity: "warning", Message: fmt.Sprintf(format, args...), Start: sourcepos.ZeroIfNil(p)}
}

// FromDiagnostics associates diagnostics with a check before step ownership is assigned.
func FromDiagnostics(ds []results.Diagnostic) []Finding {
	out := make([]Finding, len(ds))
	for i, d := range ds {
		out[i] = Finding{Diagnostic: d}
	}
	return out
}

// SourcePosition returns a declaration's position, or nil when it has none.
func SourcePosition(s *workflow.String) *sourcepos.Position {
	if s == nil {
		return nil
	}
	return s.Pos
}

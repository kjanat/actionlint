// Package reporting collects rule findings and assigns workflow source positions.
package reporting

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// StepFinding associates a diagnostic with its owning step.
type StepFinding struct {
	finding.Finding
	Step workflows.BoundStep
}

// Collector attaches a rule name and step to each reported message.
type Collector struct {
	Rule     string
	Findings []StepFinding
	Step     workflows.BoundStep
}

func (c *Collector) Error(p *sourcetext.Position, text string) {
	c.Findings = append(c.Findings, StepFinding{Rule: c.Rule, Message: text, Start: sourcetext.ZeroIfNil(p), Step: c.Step})
}

func (c *Collector) Errorf(p *sourcetext.Position, format string, args ...any) {
	c.Error(p, fmt.Sprintf(format, args...))
}

// Warning constructs a warning at the supplied source position.
func Warning(rule, code string, p *sourcetext.Position, format string, args ...any) StepFinding {
	return StepFinding{Rule: rule, Code: code, Severity: "warning", Message: fmt.Sprintf(format, args...), Start: sourcetext.ZeroIfNil(p)}
}

// FromDiagnostics associates diagnostics with a check before step ownership is assigned.
func FromDiagnostics(ds []results.Diagnostic) []StepFinding {
	out := make([]StepFinding, len(ds))
	for i, d := range ds {
		out[i] = StepFinding{Diagnostic: d}
	}
	return out
}

// SourcePosition returns a declaration's position, or nil when it has none.
func SourcePosition(s *definition.String) *sourcetext.Position {
	if s == nil {
		return nil
	}
	return s.Pos
}

// Package reporting collects rule findings and assigns workflow source positions.
package reporting

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

type StepFinding struct {
	finding.Finding
	Step workflows.BoundStep
}

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

func Warning(rule, code string, p *sourcetext.Position, format string, args ...any) StepFinding {
	return StepFinding{Rule: rule, Code: code, Severity: severity.Warning, Message: fmt.Sprintf(format, args...), Start: sourcetext.ZeroIfNil(p)}
}

func FromDiagnostics(ds []results.Diagnostic) []StepFinding {
	out := make([]StepFinding, len(ds))
	for i, d := range ds {
		out[i] = StepFinding{Diagnostic: d}
	}
	return out
}

func SourcePosition(s *definition.String) *sourcetext.Position {
	if s == nil {
		return nil
	}
	return s.Pos
}

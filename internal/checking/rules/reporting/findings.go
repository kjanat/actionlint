// Package reporting collects rule findings and assigns workflow source positions.
package reporting

import (
	"fmt"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflow "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Finding associates a diagnostic with its owning step.
type Finding struct {
	results.Diagnostic
	Step declaration.StepView
}

// Collector attaches a rule name and step to each reported message.
type Collector struct {
	Rule     string
	Findings []Finding
	Step     declaration.StepView
}

func (c *Collector) Error(p *workflow.Pos, text string) {
	c.Findings = append(c.Findings, Finding{Rule: c.Rule, Message: text, Start: Position(p), Step: c.Step})
}

func (c *Collector) Errorf(p *workflow.Pos, format string, args ...any) {
	c.Error(p, fmt.Sprintf(format, args...))
}

// Position converts a workflow position to a diagnostic position. Nil means no position.
func Position(p *workflow.Pos) sourcepos.Position {
	if p == nil {
		return sourcepos.Position{}
	}
	return sourcepos.Position{Line: p.Line, Column: p.Col}
}

// ViewPosition returns nil when a view has no source position.
func ViewPosition(pos workflow.Pos, present bool) *workflow.Pos {
	if !present {
		return nil
	}
	return &pos
}

// Warning constructs a warning at the supplied source position.
func Warning(rule, code string, p *workflow.Pos, format string, args ...any) Finding {
	return Finding{Rule: rule, Code: code, Severity: "warning", Message: fmt.Sprintf(format, args...), Start: Position(p)}
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
func SourcePosition(view interface{ Pos() (workflow.Pos, bool) }) *workflow.Pos {
	position, present := view.Pos()
	if !present {
		return nil
	}
	return &position
}

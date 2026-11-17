package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Base accumulates findings and provides default callbacks for synchronous visitors.
// Configuration, logging, scheduling and resource ownership belong to callers.
type Base struct {
	name        string
	description string
	findings    Findings
}

// NewBase initializes a visitor accumulator with its rule identity.
func NewBase(name, desc string) Base {
	return Base{name: name, description: desc}
}

// Report records findings produced outside the positional helpers and takes
// ownership of them. A finding that names no rule is attributed to this one.
func (r *Base) Report(findings ...*finding.Finding) {
	for _, finding := range findings {
		if finding != nil && finding.Rule == "" {
			finding.Rule = r.name
		}
	}
	r.findings.Report(findings...)
}

// VisitStep is the default callback for a step.
func (r *Base) VisitStep(node workflows.BoundStep) error { return nil }

// VisitJobPre is the default callback before visiting a job's children.
func (r *Base) VisitJobPre(node workflows.BoundJob) error { return nil }

// VisitJobPost is the default callback after visiting a job's children.
func (r *Base) VisitJobPost(node workflows.BoundJob) error { return nil }

// VisitWorkflowPre is the default callback before visiting a workflow's children.
func (r *Base) VisitWorkflowPre(node workflows.BoundWorkflow) error { return nil }

// VisitWorkflowPost is the default callback after visiting a workflow's children.
func (r *Base) VisitWorkflowPost(node workflows.BoundWorkflow) error { return nil }

// Error records a finding at pos. Errs returns the recorded findings.
func (r *Base) Error(pos *sourcetext.Position, msg string) {
	r.findings.Report(finding.At(pos, r.name, msg))
}

// Errorf formats and records a finding at pos. Errs returns the recorded findings.
func (r *Base) Errorf(pos *sourcetext.Position, format string, args ...any) {
	r.findings.Report(finding.Errorf(pos, r.name, format, args...))
}

// ErrorfRange reports an error with an exclusive source-range end.
func (r *Base) ErrorfRange(start, end *sourcetext.Position, format string, args ...any) {
	err := finding.Errorf(start, r.name, format, args...)
	err.EndAt(end)
	r.findings.Report(err)
}

// Errs returns copies of the findings reported by the rule, detached from it.
func (r *Base) Errs() []*finding.Finding {
	return r.findings.Errs()
}

// FindingCount returns the number of findings reported by the rule.
func (r *Base) FindingCount() int { return r.findings.FindingCount() }

// AnnotateSince annotates the findings reported after mark, a prior FindingCount.
func (r *Base) AnnotateSince(mark int, annotate func(*finding.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}

// Name returns the name of the rule.
func (r *Base) Name() string {
	return r.name
}

// Description returns the description of the rule.
func (r *Base) Description() string {
	return r.description
}

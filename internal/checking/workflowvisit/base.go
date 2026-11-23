package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

type Base struct {
	name        string
	description string
	findings    Findings
}

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

func (r *Base) VisitStep(node workflows.BoundStep) error { return nil }

func (r *Base) VisitJobPre(node workflows.BoundJob) error { return nil }

func (r *Base) VisitJobPost(node workflows.BoundJob) error { return nil }

func (r *Base) VisitWorkflowPre(node workflows.BoundWorkflow) error { return nil }

func (r *Base) VisitWorkflowPost(node workflows.BoundWorkflow) error { return nil }

func (r *Base) Error(pos *sourcetext.Position, msg string) {
	r.findings.Report(finding.At(pos, r.name, msg))
}

func (r *Base) Errorf(pos *sourcetext.Position, format string, args ...any) {
	r.findings.Report(finding.Errorf(pos, r.name, format, args...))
}

func (r *Base) ErrorfRange(start, end *sourcetext.Position, format string, args ...any) {
	err := finding.Errorf(start, r.name, format, args...)
	err.EndAt(end)
	r.findings.Report(err)
}

// Errs returns copies of the findings reported by the rule, detached from it.
func (r *Base) Errs() []*finding.Finding {
	return r.findings.Errs()
}

func (r *Base) FindingCount() int { return r.findings.FindingCount() }

// AnnotateSince annotates the findings reported after mark, a prior FindingCount.
func (r *Base) AnnotateSince(mark int, annotate func(*finding.Finding)) {
	r.findings.AnnotateSince(mark, annotate)
}

func (r *Base) Name() string {
	return r.name
}

func (r *Base) Description() string {
	return r.description
}

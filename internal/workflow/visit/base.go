package visit

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// Base accumulates findings and provides default callbacks for synchronous visitors.
// Configuration, logging, scheduling and resource ownership belong to callers.
type Base struct {
	name        string
	description string
	findings    Findings
}

// NewBase initializes a visitor accumulator with its rule identity.
func NewBase(name string, desc string) Base {
	return Base{name: name, description: desc}
}

// Report records findings produced outside the positional helpers and takes
// ownership of them.
func (r *Base) Report(findings ...*findingmodel.Finding) {
	r.findings.Report(findings...)
}

// VisitStep is callback when visiting Step node.
func (r *Base) VisitStep(node declaration.StepView) error { return nil }

// VisitJobPre is callback when visiting Job node before visiting its children.
func (r *Base) VisitJobPre(node declaration.JobView) error { return nil }

// VisitJobPost is callback when visiting Job node after visiting its children.
func (r *Base) VisitJobPost(node declaration.JobView) error { return nil }

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (r *Base) VisitWorkflowPre(node declaration.WorkflowView) error { return nil }

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (r *Base) VisitWorkflowPost(node declaration.WorkflowView) error { return nil }

// Error creates a new error from the source position and the error message and stores it in the
// rule instance. The errors can be accessed by Errs method.
func (r *Base) Error(pos *workflowlang.Pos, msg string) {
	r.findings.Report(findingmodel.At(pos, r.name, msg))
}

// Errorf reports a new error with the source position and the formatted error message and stores it
// in the rule instance. The errors can be accessed by Errs method.
func (r *Base) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	r.findings.Report(findingmodel.Errorf(pos, r.name, format, args...))
}

// ErrorfRange reports an error with an exclusive source-range end.
func (r *Base) ErrorfRange(start, end *workflowlang.Pos, format string, args ...any) {
	err := findingmodel.Errorf(start, r.name, format, args...)
	if end != nil {
		copied := *end
		err.EndPosition = &copied
	}
	if end != nil && end.Line == start.Line && end.Col > start.Col {
		err.EndColumn = end.Col - 1
	}
	r.findings.Report(err)
}

// Errs returns copies of the findings reported by the rule, detached from it.
func (r *Base) Errs() []*findingmodel.Finding {
	return r.findings.Errs()
}

// FindingCount returns the number of findings reported by the rule.
func (r *Base) FindingCount() int { return r.findings.FindingCount() }

// AnnotateSince annotates the findings reported after mark, a prior FindingCount.
func (r *Base) AnnotateSince(mark int, annotate func(*findingmodel.Finding)) {
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

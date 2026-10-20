package visit

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// Base accumulates findings and provides default callbacks for synchronous visitors.
// Configuration, logging, scheduling and resource ownership belong to callers.
type Base struct {
	RuleName        string
	DescriptionText string
	Findings        []*findingmodel.Finding
}

// NewBase initializes a visitor accumulator with its rule identity.
func NewBase(name string, desc string) Base {
	return Base{
		RuleName:        name,
		DescriptionText: desc,
	}
}

// VisitStep is callback when visiting Step node.
func (r *Base) VisitStep(node *workflowlang.Step) error { return nil }

// VisitJobPre is callback when visiting Job node before visiting its children.
func (r *Base) VisitJobPre(node *workflowlang.Job) error { return nil }

// VisitJobPost is callback when visiting Job node after visiting its children.
func (r *Base) VisitJobPost(node *workflowlang.Job) error { return nil }

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (r *Base) VisitWorkflowPre(node *workflowlang.Workflow) error { return nil }

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (r *Base) VisitWorkflowPost(node *workflowlang.Workflow) error { return nil }

// Error creates a new error from the source position and the error message and stores it in the
// rule instance. The errors can be accessed by Errs method.
func (r *Base) Error(pos *workflowlang.Pos, msg string) {
	err := findingmodel.At(pos, r.RuleName, msg)
	r.Findings = append(r.Findings, err)
}

// Errorf reports a new error with the source position and the formatted error message and stores it
// in the rule instance. The errors can be accessed by Errs method.
func (r *Base) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	err := findingmodel.Errorf(pos, r.RuleName, format, args...)
	r.Findings = append(r.Findings, err)
}

// ErrorfRange reports an error with an exclusive source-range end.
func (r *Base) ErrorfRange(start, end *workflowlang.Pos, format string, args ...any) {
	err := findingmodel.Errorf(start, r.RuleName, format, args...)
	if end != nil {
		copied := *end
		err.EndPosition = &copied
	}
	if end != nil && end.Line == start.Line && end.Col > start.Col {
		err.EndColumn = end.Col - 1
	}
	r.Findings = append(r.Findings, err)
}

// Errs returns errors found by the rule.
func (r *Base) Errs() []*findingmodel.Finding {
	return r.Findings
}

// Name returns the name of the rule.
func (r *Base) Name() string {
	return r.RuleName
}

// Description returns the description of the rule.
func (r *Base) Description() string {
	return r.DescriptionText
}

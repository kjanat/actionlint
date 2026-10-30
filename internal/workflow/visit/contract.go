package visit

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// Pass visits immutable declarations in source order.
type Pass interface {
	// VisitStep is callback when visiting Step node. It returns internal error when it cannot continue the process
	VisitStep(node declaration.StepView) error
	// VisitJobPre is callback when visiting Job node before visiting its children. It returns internal error when it cannot continue the process
	VisitJobPre(node declaration.JobView) error
	// VisitJobPost is callback when visiting Job node after visiting its children. It returns internal error when it cannot continue the process
	VisitJobPost(node declaration.JobView) error
	// VisitWorkflowPre is callback when visiting Workflow node before visiting its children. It returns internal error when it cannot continue the process
	VisitWorkflowPre(node declaration.WorkflowView) error
	// VisitWorkflowPost is callback when visiting Workflow node after visiting its children. It returns internal error when it cannot continue the process
	VisitWorkflowPost(node declaration.WorkflowView) error
}

// Rule is the internal adapter contract for traversal-based checks. Configuration,
// logging and resource ownership are separate optional capabilities.
// Public analyzers implement analyzer.Analyzer and do not implement this adapter.
type Rule interface {
	Pass
	// Errs returns copies of the reported findings, detached from the rule.
	Errs() []*findingmodel.Finding
	// FindingCount returns the number of reported findings; it marks a
	// position for AnnotateSince.
	FindingCount() int
	// AnnotateSince applies annotate to the findings reported after mark, in
	// place. Callers attach invocation provenance through it.
	AnnotateSince(mark int, annotate func(*findingmodel.Finding))
	Name() string
}

package visit

import (
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Pass is an interface to traverse a workflow syntax tree
type Pass interface {
	// VisitStep is callback when visiting Step node. It returns internal error when it cannot continue the process
	VisitStep(node *workflowlang.Step) error
	// VisitJobPre is callback when visiting Job node before visiting its children. It returns internal error when it cannot continue the process
	VisitJobPre(node *workflowlang.Job) error
	// VisitJobPost is callback when visiting Job node after visiting its children. It returns internal error when it cannot continue the process
	VisitJobPost(node *workflowlang.Job) error
	// VisitWorkflowPre is callback when visiting Workflow node before visiting its children. It returns internal error when it cannot continue the process
	VisitWorkflowPre(node *workflowlang.Workflow) error
	// VisitWorkflowPost is callback when visiting Workflow node after visiting its children. It returns internal error when it cannot continue the process
	VisitWorkflowPost(node *workflowlang.Workflow) error
}

// Rule is the internal adapter contract for traversal-based checks. Configuration,
// logging and resource ownership are separate optional capabilities.
// Public analyzers implement analyzer.Analyzer and do not implement this adapter.
type Rule interface {
	Pass
	Errs() []*findingmodel.Finding
	Name() string
}

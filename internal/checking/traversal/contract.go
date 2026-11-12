package visit

import (
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// Pass visits immutable declarations in source order. Each method returns an
// error if traversal cannot continue.
type Pass interface {
	// VisitStep visits a step and returns an error if traversal cannot continue.
	VisitStep(node declaration.StepView) error
	// VisitJobPre visits a job before its children.
	VisitJobPre(node declaration.JobView) error
	// VisitJobPost visits a job after its children.
	VisitJobPost(node declaration.JobView) error
	// VisitWorkflowPre visits a workflow before its children.
	VisitWorkflowPre(node declaration.WorkflowView) error
	// VisitWorkflowPost visits a workflow after its children.
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

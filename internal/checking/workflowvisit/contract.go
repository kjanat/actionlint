package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Pass visits immutable declarations in source order. Each method returns an
// error if traversal cannot continue.
type Pass interface {
	// VisitStep visits a step and returns an error if traversal cannot continue.
	VisitStep(node workflows.Step) error
	// VisitJobPre visits a job before its children.
	VisitJobPre(node workflows.Job) error
	// VisitJobPost visits a job after its children.
	VisitJobPost(node workflows.Job) error
	// VisitWorkflowPre visits a workflow before its children.
	VisitWorkflowPre(node workflows.Workflow) error
	// VisitWorkflowPost visits a workflow after its children.
	VisitWorkflowPost(node workflows.Workflow) error
}

// Rule is the internal adapter contract for traversal-based checks. Configuration,
// logging and resource ownership are separate optional capabilities.
// Public analyzers implement analyzer.Analyzer and do not implement this adapter.
type Rule interface {
	Pass
	// Errs returns copies of the reported findings, detached from the rule.
	Errs() []*finding.Finding
	// FindingCount returns the number of reported findings; it marks a
	// position for AnnotateSince.
	FindingCount() int
	// AnnotateSince applies annotate to the findings reported after mark, in
	// place. Callers attach invocation provenance through it.
	AnnotateSince(mark int, annotate func(*finding.Finding))
	Name() string
}

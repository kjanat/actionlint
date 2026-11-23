package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Pass visits immutable declarations in source order.
type Pass interface {
	VisitStep(node workflows.BoundStep) error
	VisitJobPre(node workflows.BoundJob) error
	VisitJobPost(node workflows.BoundJob) error
	VisitWorkflowPre(node workflows.BoundWorkflow) error
	VisitWorkflowPost(node workflows.BoundWorkflow) error
}

type Rule interface {
	Pass
	// Errs returns copies detached from the rule.
	Errs() []*finding.Finding
	FindingCount() int
	// AnnotateSince applies annotate in place to the findings reported after mark.
	AnnotateSince(mark int, annotate func(*finding.Finding))
	Name() string
}

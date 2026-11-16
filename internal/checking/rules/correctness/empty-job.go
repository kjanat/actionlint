package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyJobRule is the single definition of the empty-job rule.
var EmptyJobRule = &define.Rule{
	Name:        "empty-job",
	Description: "Report jobs without steps",
	Category:    "correctness",
	Subcategory: "jobs",
	Recommended: true,
	Explanation: "GitHub rejects jobs without steps unless they call a reusable workflow.",
	Help:        "Add steps, call a reusable workflow with uses, or remove the job.",
	Examples: []rule.Example{rule.WorkflowExample("Give the job steps to run.",
		"    steps: []\n", "")},
	Checks: define.Fixed(define.Checks{Job: EmptyJob}),
}

// EmptyJob reports a job that runs no steps. Reusable workflow calls have none.
func EmptyJob(job declaration.Job) []reporting.StepFinding {
	if job.WorkflowCall != nil || job.DeclaredUses != nil {
		return nil
	}
	for range job.Steps() {
		return nil
	}
	return []reporting.StepFinding{{Severity: "error", Start: reporting.Position(job.Pos), Message: fmt.Sprintf("job %q must define at least one step or call a reusable workflow with uses", job.ID.Text())}}
}

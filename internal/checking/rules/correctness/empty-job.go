package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var EmptyJobRule = &define.Rule{
	Name:        "empty-job",
	Description: "Report jobs without steps",
	Category:    "correctness",
	Subcategory: "jobs",
	LegacyGroup: "style",
	Recommended: true,
	Explanation: "GitHub rejects jobs without steps unless they call a reusable workflow.",
	Help:        "Add steps, call a reusable workflow with uses, or remove the job.",
	Examples: []rule.Example{rule.WorkflowExample("",
		"    steps: []\n", "")},
	Checks: define.Fixed(define.Checks{Job: EmptyJob}),
}

func EmptyJob(job workflows.BoundJob) []reporting.StepFinding {
	if job.WorkflowCall != nil || job.DeclaredUses != nil {
		return nil
	}
	for range job.Steps() {
		return nil
	}
	return []reporting.StepFinding{{Severity: severity.Error, Start: sourcetext.ZeroIfNil(job.Pos), Message: fmt.Sprintf("job %q must define at least one step or call a reusable workflow with uses", job.ID.Text())}}
}

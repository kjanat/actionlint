package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyJobRule is the single definition of the empty-job rule.
var EmptyJobRule = &define.Rule{Name: "empty-job", Description: "Report jobs without steps", Category: "correctness", Subcategory: "jobs", Recommended: true, Explanation: "GitHub rejects jobs without steps unless they call a reusable workflow.", Help: "Add steps, call a reusable workflow with uses, or remove the job.", Examples: []rule.Example{rule.WorkflowExample("Give the job steps to run.",
	"    steps: []\n", "")},
	Checks: define.Fixed(define.Checks{Job: EmptyJob}),
}

package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// JobNeedsRule is the single definition of the job-needs rule.
var JobNeedsRule = &define.Rule{Name: "job-needs", Description: "Report undefined job dependencies and dependency cycles", Category: "correctness", Subcategory: "jobs", Recommended: true, Help: "Reference an existing job ID and remove dependency cycles.", Examples: []rule.Example{rule.WorkflowExample("Remove a dependency on a job that does not exist.",
	"    needs: missing\n", "")},
	Checks: define.Fixed(define.Checks{Workflow: checkJobDependencies}),
}

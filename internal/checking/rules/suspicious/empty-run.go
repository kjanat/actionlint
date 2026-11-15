package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyRunRule is the single definition of the empty-run rule.
var EmptyRunRule = &define.Rule{Name: "empty-run", Description: "Report steps with an empty run script", Category: "suspicious", Subcategory: "steps", Recommended: true, Explanation: "Report run steps whose script is empty. GitHub accepts the step, but it runs nothing.", Help: "Write the commands the step should run, or remove the step.", Examples: []rule.Example{rule.WorkflowExample("Write the commands the step runs.",
	"    steps:\n      - run:\n", "    steps:\n      - run: echo ok\n")},
	Checks: define.Fixed(define.Checks{Step: EmptyRun}),
}

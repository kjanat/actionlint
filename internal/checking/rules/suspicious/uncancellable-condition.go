package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// UncancellableConditionRule is the single definition of the uncancellable-condition rule.
var UncancellableConditionRule = &define.Rule{
	Name:        "uncancellable-condition",
	Description: "Review jobs that can continue after cancellation",
	Category:    "suspicious",
	Subcategory: "expressions",
	Explanation: "Opt-in policy for job conditions using always() that can evaluate to true during cancellation. Cleanup and cancellation notifications can require this behavior; the condition alone does not prove a mistake. Step conditions are not reported because action names do not establish whether their work is needed for cleanup. cancel-timeout-minutes does not force cancellation of a job whose condition keeps it running. Warning when enabled; --strict enables this policy as an error, including intentional uses.",
	Help:        "If cancellation should stop the job, use !cancelled(). Keep always() when cancellation cleanup or reporting must run. A standalone always() offers a behavior-changing fix requiring explicit review; compound conditions have no replacement. See [GitHub's always() guidance](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#always) and the [job-versus-step cancellation test](https://github.com/kjanat/actionlint/actions/runs/41520665123).",
	Examples: []rule.Example{rule.WorkflowExample("When this job should stop on cancellation, use !cancelled(). Do not make this change for required cancellation cleanup or reporting.",
		"    if: ${{ always() }}\n", "    if: ${{ !cancelled() }}\n")},
	DefaultLevel: "on",
	Checks:       define.Fixed(define.Checks{Job: checkJobCancellationCondition}),
}

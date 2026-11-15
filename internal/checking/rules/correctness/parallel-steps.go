package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// ParallelStepsRule is the single definition of the parallel-steps rule.
var ParallelStepsRule = &define.Rule{
	Name:        "parallel-steps",
	Description: "Validate background-step references and restrictions within parallel groups",
	Category:    "correctness",
	Subcategory: "steps",
	Recommended: true,
	Help:        "Check background-step names and wait/cancel references in the parallel group.",
	Checks:      define.Fixed(define.Checks{Job: ParallelSteps}),
}

package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// IfCondRule is the single definition of the if-cond rule.
var IfCondRule = &define.Rule{Name: "if-cond", Description: "Report conditions that always evaluate to true or false", Category: "correctness", Subcategory: "expressions", Recommended: true, Help: "Correct the expression to test the intended runtime value. Remove a condition that is always true if it is unnecessary.", Examples: []rule.Example{rule.WorkflowExample("Remove an unconditional truthy condition.",
	"    if: true\n", "")},
	Checks: define.Fixed(define.Checks{Job: checkJobConditions, Step: checkStepCondition}),
}

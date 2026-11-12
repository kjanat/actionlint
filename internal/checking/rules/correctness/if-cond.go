package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// IfCondDeclaration describes the if-cond rule.
func IfCondDeclaration() rule.Declaration {
	return rule.Declaration{Name: "if-cond", Description: "Report conditions that always evaluate to true or false", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report conditions that always evaluate to true or false.", Help: "Correct the expression to test the intended runtime value. Remove a condition that is always true if it is unnecessary.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#if-cond", Examples: []rule.Example{rule.WorkflowExample("Remove an unconditional truthy condition.",
		"    if: true\n", "")}}
}

// IfCondCheck binds the rule to job and step declarations.
func IfCondCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: IfCondDeclaration(), Job: checkJobConditions, Step: checkStepCondition}
}

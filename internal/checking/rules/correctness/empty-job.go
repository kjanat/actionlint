package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyJobDeclaration describes the empty-job rule.
func EmptyJobDeclaration() rule.Declaration {
	return rule.Declaration{Name: "empty-job", Description: "Report jobs without steps", Category: "correctness", Subcategory: "jobs", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "GitHub rejects jobs without steps unless they call a reusable workflow.", Help: "Add steps, call a reusable workflow with uses, or remove the job.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#empty-job", Examples: []rule.Example{rule.WorkflowExample("Give the job steps to run.",
		"    steps: []\n", "")}}
}

// EmptyJobCheck binds the rule to job declarations.
func EmptyJobCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: EmptyJobDeclaration(), Job: EmptyJob}
}

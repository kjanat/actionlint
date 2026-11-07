package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyJobDeclaration describes the empty-job rule.
func EmptyJobDeclaration() rule.Declaration {
	return rule.Declaration{Name: "empty-job", Description: "Report jobs without steps", Category: "suspicious", Subcategory: "jobs", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report jobs that run no steps. GitHub accepts the job, but it does nothing.", Help: "Add the steps the job should run, or remove the job.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#empty-job", Examples: []rule.Example{rule.WorkflowExample("Give the job steps to run.",
		"    steps: []\n", "")}}
}

// EmptyJobCheck binds the rule to job declarations.
func EmptyJobCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: EmptyJobDeclaration(), Job: EmptyJob}
}

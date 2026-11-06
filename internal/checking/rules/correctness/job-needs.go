package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// JobNeedsDeclaration describes the job-needs rule.
func JobNeedsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "job-needs", Description: "Report undefined job dependencies and dependency cycles", Category: "correctness", Subcategory: "jobs", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report undefined job dependencies and dependency cycles.", Help: "Reference an existing job ID and remove dependency cycles.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#job-needs", Examples: []rule.Example{rule.WorkflowExample("Remove a dependency on a job that does not exist.",
		"    needs: missing\n", "")}}
}

// JobNeedsCheck binds the rule to workflow declarations.
func JobNeedsCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: JobNeedsDeclaration(), Workflow: checkJobDependencies}
}

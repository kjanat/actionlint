package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EnvVarDeclaration describes the env-var rule.
func EnvVarDeclaration() rule.Declaration {
	return rule.Declaration{Name: "env-var", Description: "Validate environment variable declarations", Category: "correctness", Subcategory: "environment", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate environment variable declarations.", Help: "Use valid environment variable names and values in the indicated mapping.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#env-var", Examples: []rule.Example{rule.WorkflowExample("Use environment names without spaces.",
		"    env:\n      BAD NAME: value\n", "    env:\n      GOOD_NAME: value\n")}}
}

// EnvVarCheck binds the rule to workflow, job, and step declarations.
func EnvVarCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: EnvVarDeclaration(), Workflow: checkWorkflowEnvironment, Job: checkJobEnvironment, Step: checkStepEnvironment}
}

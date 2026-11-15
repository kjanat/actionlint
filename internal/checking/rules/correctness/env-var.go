package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EnvVarRule is the single definition of the env-var rule.
var EnvVarRule = &define.Rule{
	Name:        "env-var",
	Description: "Validate environment variable declarations",
	Category:    "correctness",
	Subcategory: "environment",
	Recommended: true,
	Help:        "Use valid environment variable names and values in the indicated mapping.",
	Examples: []rule.Example{rule.WorkflowExample("Use environment names without spaces.",
		"    env:\n      BAD NAME: value\n", "    env:\n      GOOD_NAME: value\n")},
	Checks: define.Fixed(define.Checks{Workflow: checkWorkflowEnvironment, Job: checkJobEnvironment, Step: checkStepEnvironment}),
}

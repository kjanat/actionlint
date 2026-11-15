package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// IdRule is the single definition of the id rule.
var IdRule = &define.Rule{
	Name:        "id",
	Description: "Report invalid job or step IDs and duplicate step IDs",
	Category:    "correctness",
	Subcategory: "identity",
	Recommended: true,
	Help:        "Use a unique valid identifier within its job or workflow scope.",
	Examples: []rule.Example{rule.WorkflowExample("Begin step IDs with a letter or underscore.",
		"    steps:\n      - id: 1build\n        run: echo ok\n", "    steps:\n      - id: build\n        run: echo ok\n")},
	Checks: define.Fixed(define.Checks{Job: checkIdentifiers}),
}

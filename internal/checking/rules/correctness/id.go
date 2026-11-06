package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// IdDeclaration describes the id rule.
func IdDeclaration() rule.Declaration {
	return rule.Declaration{Name: "id", Description: "Report invalid job or step IDs and duplicate step IDs", Category: "correctness", Subcategory: "identity", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report invalid job or step IDs and duplicate step IDs.", Help: "Use a unique valid identifier within its job or workflow scope.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#id", Examples: []rule.Example{rule.WorkflowExample("Begin step IDs with a letter or underscore.",
		"    steps:\n      - id: 1build\n        run: echo ok\n", "    steps:\n      - id: build\n        run: echo ok\n")}}
}

// IdCheck binds the rule to job declarations.
func IdCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: IdDeclaration(), Job: checkIdentifiers}
}

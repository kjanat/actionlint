package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyTriggersDeclaration describes the empty-triggers rule.
func EmptyTriggersDeclaration() rule.Declaration {
	return rule.Declaration{Name: "empty-triggers", Description: "Report workflows that declare no triggers", Category: "suspicious", Subcategory: "triggers", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report workflows whose on section is empty. GitHub accepts the workflow, but no event ever starts it.", Help: "Add the events that should start the workflow, or delete the workflow.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#empty-triggers", Examples: []rule.Example{{
		Description: "Declare the events that start the workflow.",
		Before:      "on:\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
		After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
	}}}
}

// EmptyTriggersCheck binds the rule to workflow declarations.
func EmptyTriggersCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: EmptyTriggersDeclaration(), Workflow: EmptyTriggers}
}

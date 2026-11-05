package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// EmptyRunDeclaration describes the empty-run rule.
func EmptyRunDeclaration() rule.Declaration {
	return rule.Declaration{Name: "empty-run", Description: "Report steps with an empty run script", Category: "suspicious", Subcategory: "steps", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report run steps whose script is empty. GitHub accepts the step, but it runs nothing.", Help: "Write the commands the step should run, or remove the step.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#empty-run", Examples: []rule.Example{rule.WorkflowExample("Write the commands the step runs.",
		"    steps:\n      - run:\n", "    steps:\n      - run: echo ok\n")}}
}

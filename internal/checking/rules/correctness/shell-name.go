package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ShellNameDeclaration describes the shell-name rule.
func ShellNameDeclaration() rule.Declaration {
	return rule.Declaration{Name: "shell-name", Description: "Validate script shells for the selected runner", Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate script shells for the selected runner.", Help: "Select a supported shell for the runner, or provide an explicit shell command containing {0}.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#shell-name", Examples: []rule.Example{rule.WorkflowExample("Correct the shell name.",
		"    steps:\n      - shell: bashh\n        run: echo ok\n", "    steps:\n      - shell: bash\n        run: echo ok\n")}}
}

package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ShellNameDeclaration describes the shell-name rule.
func ShellNameDeclaration() rule.Declaration {
	return rule.Declaration{Name: "shell-name", Description: "Validate script shells for the selected runner", Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate shell names for the selected runner and interpreter options supplied by built-in or plugin definitions. The rule cannot validate options when no interpreter definition matches or an argument contains an unresolved expression.", Help: "Select a supported interpreter, include {0} for the script filename in custom commands, and correct invalid options. Bash uses -o for set options and -O for shopt options.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#shell-name", Examples: []rule.Example{rule.WorkflowExample("Correct the shell name.",
		"    steps:\n      - shell: bashh\n        run: echo ok\n", "    steps:\n      - shell: bash\n        run: echo ok\n")}}
}

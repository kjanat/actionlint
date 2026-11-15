package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

// ShellNameRule is the single definition of the shell-name rule.
var ShellNameRule = &define.Rule{
	Name:        "shell-name",
	Description: "Validate script shells for the selected runner",
	Category:    "correctness",
	Subcategory: "scripts",
	Recommended: true,
	Explanation: "Validate shell names for the selected runner and interpreter options supplied by built-in or plugin definitions. The rule cannot validate options when no interpreter definition matches or an argument contains an unresolved expression.",
	Help:        "Select a supported interpreter, include {0} for the script filename in custom commands, and correct invalid options. Bash uses -o for set options and -O for shopt options.",
	Examples: []rule.Example{rule.WorkflowExample("Correct the shell name.",
		"    steps:\n      - shell: bashh\n        run: echo ok\n", "    steps:\n      - shell: bash\n        run: echo ok\n")},
	Checks: func(c define.Inputs) define.Checks {
		return ShellNameChecks(c.Runners, commands.FromContext(c.Context))
	},
}

package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ErrexitStatusDeclaration describes status captures that lose failure statuses.
func ErrexitStatusDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "errexit-status", Description: "Report Bash status reads that cannot capture a preceding failure",
		Category: "suspicious", Subcategory: "scripts", Maturity: "nursery", Provider: "actionlint", Recommended: false, DefaultLevel: "warn",
		Explanation: "With Bash errexit enabled, a failing simple command exits before a following $? read. " +
			"The read can capture success, but cannot capture the preceding failure.\n\n" +
			"The check uses registered interpreter options and follows literal set changes, groups, subshells and known branches. " +
			"This warning is opt-in because a success-only read can be intentional.\n\n" +
			"The check excludes reads within condition tests, AND/OR lists and pipelines, and reads of compound-command results. " +
			"It skips trap bodies, function bodies and command substitutions. Unknown option values suspend analysis until a known set change. " +
			"Operations that may replace builtins or install traps stop further analysis in that scope.\n\n" +
			"The rule is experimental because function calls, command substitution inheritance and dynamic option effects are not modeled. " +
			"Bash behavior is specified at https://www.gnu.org/software/bash/manual/html_node/The-Set-Builtin.html; runner defaults are in actions/runner ScriptHandlerHelpers.cs.",
		Help:             "Capture the failure status in an if condition or an || handler when the script must continue after failure.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#errexit-status",
		SupportsActions:  true, Scope: rule.SourceConfiguration, RequiredInputs: []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
		Examples: []rule.Example{rule.WorkflowExample("Capture failure explicitly when the script must continue.",
			"    steps:\n      - shell: bash\n        run: |\n          CHANGED=\"$(git diff --exit-code)\"; EXIT_CODE=$?\n",
			"    steps:\n      - shell: bash\n        run: |\n          CHANGED=\"$(git diff --exit-code)\" || EXIT_CODE=$?\n")},
	}
}

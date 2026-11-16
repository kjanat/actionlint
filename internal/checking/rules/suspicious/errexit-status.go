package suspicious

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// ErrexitStatusRule is the single definition of the errexit-status rule.
var ErrexitStatusRule = &define.Rule{
	Name:         "errexit-status",
	Description:  "Report Bash status reads that cannot capture a preceding failure",
	Category:     "suspicious",
	Subcategory:  "scripts",
	Maturity:     "nursery",
	DefaultLevel: "warn",
	Explanation: "With Bash errexit enabled, a failing simple command exits before a following $? read. " +
		"The read can capture success, but cannot capture the preceding failure.\n\n" +
		"The check uses registered interpreter options and follows literal set changes, groups, subshells and known branches. " +
		"This warning is opt-in because a success-only read can be intentional.\n\n" +
		"The check excludes reads within condition tests, AND/OR lists and pipelines, and reads of compound-command results. " +
		"It skips trap bodies, function bodies and command substitutions. Unknown option values suspend analysis until a known set change. " +
		"Operations that may replace builtins or install traps stop further analysis in that scope.\n\n" +
		"The rule is experimental because function calls, command substitution inheritance and dynamic option effects are not modeled. " +
		"Bash behavior is specified at https://www.gnu.org/software/bash/manual/html_node/The-Set-Builtin.html; runner defaults are in actions/runner ScriptHandlerHelpers.cs.",
	Help:            "Capture the failure status in an if condition or an || handler when the script must continue after failure.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
	Examples: []rule.Example{rule.WorkflowExample("Capture failure explicitly when the script must continue.",
		"    steps:\n      - shell: bash\n        run: |\n          CHANGED=\"$(git diff --exit-code)\"; EXIT_CODE=$?\n",
		"    steps:\n      - shell: bash\n        run: |\n          CHANGED=\"$(git diff --exit-code)\" || EXIT_CODE=$?\n")},
	Script: checkErrexitStatus,
}

// checkErrexitStatus reports status reads that Bash errexit makes unable to capture a failure.
func checkErrexitStatus(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return scanRuntimeScripts(ctx, input, func(script checkinputs.Script, invocation commands.Invocation, report func(scripts.ScriptRange, string)) {
		for _, span := range scripts.ErrexitStatusReads(script.Text, invocation, script.Interpreters) {
			report(span, "Bash errexit exits if the preceding command fails, so this status read cannot capture that failure. Use an explicit condition or an OR handler if the script must continue")
		}
	})
}

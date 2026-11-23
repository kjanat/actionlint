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

var ErrexitStatusRule = &define.Rule{
	Name:         "errexit-status",
	Description:  "Report Bash status reads that cannot capture a preceding failure",
	Category:     "suspicious",
	Subcategory:  "scripts",
	Maturity:     "nursery",
	DefaultLevel: "warn",
	Explanation: "With Bash errexit enabled, a failing simple command exits before a following $? read. " +
		"The read only ever sees success.",
	Help:            "Capture the status in an if condition or an || handler.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
	Script:          checkErrexitStatus,
}

func checkErrexitStatus(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return scanRuntimeScripts(ctx, input, func(script checkinputs.Script, invocation commands.Invocation, report func(scripts.ScriptRange, string)) {
		for _, span := range scripts.ErrexitStatusReads(script.Text, invocation, script.Interpreters) {
			report(span, "Bash errexit exits if the preceding command fails, so this status read cannot capture that failure. Use an explicit condition or an OR handler if the script must continue")
		}
	})
}

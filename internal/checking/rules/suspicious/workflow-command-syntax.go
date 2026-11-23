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

var WorkflowCommandSyntaxRule = &define.Rule{
	Name:            "workflow-command-syntax",
	Description:     "Report malformed runner commands in literal script output",
	Category:        "suspicious",
	Subcategory:     "scripts",
	Maturity:        "nursery",
	DefaultLevel:    "warn",
	Help:            "Use ::name::message or ::name key=value::message with a runner command name. Wrap intentional command examples in stop-commands.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations, rule.Execution},
	Examples: []rule.Example{rule.WorkflowExample("Close the annotation name with two colons.",
		"    steps:\n      - shell: bash\n        run: echo \"::notice:Up to date\"\n",
		"    steps:\n      - shell: bash\n        run: echo \"::notice::Up to date\"\n")},
	Script: checkWorkflowCommandSyntax,
}

func checkWorkflowCommandSyntax(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return scanRuntimeScripts(ctx, input, func(script checkinputs.Script, invocation commands.Invocation, report func(scripts.ScriptRange, string)) {
		if invocation.Status == commands.Unknown && invocation.Reason == "no interpreter definition matches the command" {
			// The runner recognizes these exact built-in shell names even when
			// the corresponding optional language plugin is not installed.
			switch script.Runtime.Shell.Name {
			case "python", "pwsh", "powershell":
				invocation = commands.Invocation{Status: commands.Valid, Language: script.Runtime.Shell.Name, Input: "file"}
			}
		}
		seen := make(map[scripts.ScriptRange]bool)
		for _, path := range scripts.LiteralStdoutPaths(script.Text, invocation, script.Interpreters) {
			var state commands.WorkflowCommandState
			for _, output := range path {
				if problem := state.WorkflowCommandProblem(output.Text); problem != "" && !seen[output.ScriptRange] {
					report(output.ScriptRange, problem)
					seen[output.ScriptRange] = true
				}
			}
		}
	})
}

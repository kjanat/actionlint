package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
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
	Checks: func(c define.RuleInputs) define.Checks {
		return ShellNameChecks(c.Runners, commands.FromContext(c.Context))
	},
}

// ShellNameChecks validates shell declarations against their declaring job's
// prepared platform. Without a known job platform, it accepts any supported shell.
func ShellNameChecks(runners *runnerlang.Knowledge, registries ...*commands.Registry) define.Checks {
	platform := func(job workflowdecl.Job) runnerlang.Platform {
		if runners == nil || !job.Present() {
			return runnerlang.Any
		}
		return runners.Job(job).Platform
	}
	return define.Checks{
		Workflow: func(workflow workflowdecl.Workflow) []reporting.StepFinding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(workflowdecl.DefaultsRun(workflow.Defaults)), runnerlang.Any, registries...))
		},
		Job: func(job workflowdecl.Job) []reporting.StepFinding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(workflowdecl.DefaultsRun(job.Defaults)), platform(job), registries...))
		},
		Step: func(step workflowdecl.Step) []reporting.StepFinding {
			if run, present := step.Run(); present {
				findings := reporting.FromDiagnostics(checkShell(workflowdecl.Scalar(run.Shell), platform(step.Job()), registries...))
				for i := range findings {
					findings[i].Step = step
				}
				return findings
			}
			return nil
		},
	}
}

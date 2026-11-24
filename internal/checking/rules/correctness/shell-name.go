package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

var ShellNameRule = &define.Rule{
	SupportsActions: true,
	Name:            "shell-name",
	Description:     "Validate script shells for the selected runner",
	Category:        "correctness",
	Subcategory:     "scripts",
	Recommended:     true,
	Help:            "Use a supported shell, include {0} for the script filename in custom commands, and fix invalid options. Bash takes -o for set options and -O for shopt options.",
	Examples: []rule.Example{rule.WorkflowExample("",
		"    steps:\n      - shell: bashh\n        run: echo ok\n", "    steps:\n      - shell: bash\n        run: echo ok\n")},
	Checks: func(c define.RuleInputs) define.Checks {
		return ShellNameChecks(c.Runners, commands.FromContext(c.Context))
	},
}

// ShellNameChecks accepts any supported shell when the job platform is unknown.
func ShellNameChecks(knownRunners *runners.Knowledge, registries ...*commands.Registry) define.Checks {
	platform := func(job workflows.BoundJob) runners.Platform {
		if knownRunners == nil || !job.Present() {
			return runners.Any
		}
		return knownRunners.Job(job).Platform
	}
	return define.Checks{
		WorkflowStepsOnly: true,
		Workflow: func(workflow workflows.BoundWorkflow) []reporting.StepFinding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(workflows.DefaultsRun(workflow.Defaults)), runners.Any, registries...))
		},
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(workflows.DefaultsRun(job.Defaults)), platform(job), registries...))
		},
		Step: func(step workflows.BoundStep) []reporting.StepFinding {
			if run, present := step.Run(); present {
				findings := reporting.FromDiagnostics(checkShell(workflows.Scalar(run.Shell), platform(step.Job()), registries...))
				for i := range findings {
					findings[i].Step = step
				}
				return findings
			}
			return nil
		},
	}
}

package correctness

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

// ShellNameCheck validates shell declarations against their declaring job's
// prepared platform. Without a known job platform, it accepts any supported shell.
func ShellNameCheck(runners *runnerlang.Knowledge, registries ...*commands.Registry) checkinputs.DeclarationCheck {
	platform := func(job workflowdecl.Job) runnerlang.Platform {
		if runners == nil || !job.Present() {
			return runnerlang.Any
		}
		return runners.Job(job).Platform
	}
	return checkinputs.DeclarationCheck{
		Declaration: ShellNameDeclaration(),
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

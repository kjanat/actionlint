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
	platform := func(job workflowdecl.JobView) runnerlang.Platform {
		if runners == nil || !job.Present() {
			return runnerlang.Any
		}
		return runners.Job(job).Platform
	}
	return checkinputs.DeclarationCheck{
		Declaration: ShellNameDeclaration(),
		Workflow: func(workflow workflowdecl.WorkflowView) []reporting.Finding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(workflow.Defaults().Run().Semantic()), runnerlang.Any, registries...))
		},
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			return reporting.FromDiagnostics(checkShell(execution.DefaultShell(job.Defaults().Run().Semantic()), platform(job), registries...))
		},
		Step: func(step workflowdecl.StepView) []reporting.Finding {
			if run, present := step.Run(); present {
				findings := reporting.FromDiagnostics(checkShell(run.Shell().Semantic(), platform(step.Job()), registries...))
				for i := range findings {
					findings[i].Step = step
				}
				return findings
			}
			return nil
		},
	}
}

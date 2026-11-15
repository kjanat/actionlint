package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	cachemodel "actionlint.kjanat.dev/v2/internal/model/workflows/cache"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// CacheOperationCheck checks action operations and reusable calls against their
// effective cache grant. Each callback derives its job mode from its own input.
func CacheOperationCheck(workflow workflowdecl.Workflow, lookup *workflowinputs.ResolvedWorkflows) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: CacheOperationDeclaration(),
		Job: func(job workflowdecl.Job) []reporting.StepFinding {
			if lookup == nil {
				return nil
			}
			mode := cachemodel.EffectiveMode(workflow.CacheMode, job.CacheMode)
			return reporting.FromDiagnostics(cachepolicy.CheckCallOperations(job.WorkflowCall, mode, lookup))
		},
		Step: func(step workflowdecl.Step) []reporting.StepFinding {
			// Composite action steps have no declaring workflow job, so the
			// job layer contributes no cache override.
			var jobMode *workflowlang.CacheMode
			if job := step.Job(); job.Present() {
				jobMode = job.CacheMode
			}
			mode := cachemodel.EffectiveMode(workflow.CacheMode, jobMode)
			findings := reporting.FromDiagnostics(cachepolicy.CheckStep(step, mode))
			for i := range findings {
				findings[i].Step = step
			}
			return findings
		},
	}
}

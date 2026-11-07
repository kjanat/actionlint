package correctness

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowinputs "actionlint.kjanat.dev/v2/internal/input/references/workflows"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	cachemodel "actionlint.kjanat.dev/v2/internal/model/workflows/cache"
)

// CacheOperationCheck checks action operations and reusable calls against their
// effective cache grant. Each callback derives its job mode from its own input.
func CacheOperationCheck(workflow workflowdecl.WorkflowView, lookup *workflowinputs.ResolvedWorkflows) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: CacheOperationDeclaration(),
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			if lookup == nil {
				return nil
			}
			mode := cachemodel.EffectiveMode(workflow.CacheMode(), job.CacheMode())
			return reporting.FromDiagnostics(cachepolicy.CheckCallOperations(job.WorkflowCall(), mode, lookup))
		},
		Step: func(step workflowdecl.StepView) []reporting.Finding {
			mode := cachemodel.EffectiveMode(workflow.CacheMode(), step.Job().CacheMode())
			findings := reporting.FromDiagnostics(cachepolicy.CheckStep(step, mode))
			for i := range findings {
				findings[i].Step = step
			}
			return findings
		},
	}
}

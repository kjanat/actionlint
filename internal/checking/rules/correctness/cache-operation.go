package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/references/reusableworkflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/cachegrant"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// CacheOperationRule is the single definition of the cache-operation rule.
var CacheOperationRule = &define.Rule{
	Name:        "cache-operation",
	Description: "Report cache actions blocked by explicit cache access limits",
	Category:    "correctness",
	Subcategory: "cache",
	Policy:      "cache-operation",
	Recommended: true,
	Explanation: "Report cache save or restore operations blocked by an explicit cache mode. GitHub skips the blocked operations.",
	Help:        "Remove the cache step or choose an action and cache mode that allow the intended operation.",
	Prepare:     define.PrepareWorkflowMetadata,
	Checks: func(c define.RuleInputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheOperationChecks(workflow, c.Workflows)
	},
	Enabled: func(c define.RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-operation") },
}

// CacheOperationChecks checks action operations and reusable calls against their
// effective cache grant. Each callback derives its job mode from its own input.
func CacheOperationChecks(workflow workflows.BoundWorkflow, lookup *reusableworkflows.ResolvedWorkflows) define.Checks {
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			if lookup == nil {
				return nil
			}
			mode := cachegrant.EffectiveMode(workflow.CacheMode, job.CacheMode)
			return reporting.FromDiagnostics(cachepolicy.CheckCallOperations(job.WorkflowCall, mode, lookup))
		},
		Step: func(step workflows.BoundStep) []reporting.StepFinding {
			// Composite action steps have no declaring workflow job, so the
			// job layer contributes no cache override.
			var jobMode *definition.CacheMode
			if job := step.Job(); job.Present() {
				jobMode = job.CacheMode
			}
			mode := cachegrant.EffectiveMode(workflow.CacheMode, jobMode)
			findings := reporting.FromDiagnostics(cachepolicy.CheckStep(step, mode))
			for i := range findings {
				findings[i].Step = step
			}
			return findings
		},
	}
}

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

var CacheOperationRule = &define.Rule{
	Name:        "cache-operation",
	Description: "Report cache actions blocked by explicit cache access limits",
	Category:    "correctness",
	Subcategory: "cache",
	Policy:      "cache-operation",
	Recommended: true,
	Explanation: "Report cache saves and restores that an explicit cache mode blocks. GitHub skips them.",
	Help:        "Remove the step, or change the action or cache mode to allow the operation.",
	Prepare:     define.PrepareWorkflowMetadata,
	Checks: func(c define.RuleInputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheOperationChecks(workflow, c.Workflows)
	},
	Enabled: func(c define.RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-operation") },
}

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

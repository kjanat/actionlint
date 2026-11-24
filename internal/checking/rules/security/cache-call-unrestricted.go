package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var CacheCallUnrestrictedRule = &define.Rule{
	Name:        "cache-call-unrestricted",
	Description: "Report reusable workflow calls without explicit cache limits on low-trust triggers",
	Category:    "security",
	Subcategory: "cache",
	Policy:      "cache-call-unrestricted",
	Recommended: true,
	Explanation: "Report reusable workflow calls on low-trust triggers that set no cache mode. The called workflow can then request cache writes.",
	Help:        "Set cache-mode to read or none on the calling job or workflow.",
	Checks: func(c define.RuleInputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheCallUnrestrictedChecks(workflow)
	},
	Enabled: func(c define.RuleInputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") },
}

// CacheCallUnrestrictedChecks judges trust from the supplied workflow's triggering events.
func CacheCallUnrestrictedChecks(workflow workflows.BoundWorkflow) define.Checks {
	access := cachepolicy.NewTrust(workflow)
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			return reporting.FromDiagnostics(access.CheckCall(job))
		},
	}
}

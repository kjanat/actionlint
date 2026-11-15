package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// CacheCallUnrestrictedRule is the single definition of the cache-call-unrestricted rule.
var CacheCallUnrestrictedRule = &define.Rule{
	Name:        "cache-call-unrestricted",
	Description: "Report reusable workflow calls without explicit cache limits on low-trust triggers",
	Category:    "security",
	Subcategory: "cache",
	Recommended: true,
	Explanation: "Report reusable workflow calls reachable from low-trust triggers without an explicit cache mode. The called workflow can request cache writes despite the trigger's read-only default.",
	Help:        "Set cache-mode to read or none on the calling job or workflow.",
	Checks: func(c define.Inputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheCallUnrestrictedChecks(workflow)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.CachePolicyEnabled("cache-call-unrestricted") },
}

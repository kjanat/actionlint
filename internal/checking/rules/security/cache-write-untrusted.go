package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// CacheWriteUntrustedRule is the single definition of the cache-write-untrusted rule.
var CacheWriteUntrustedRule = &define.Rule{Name: "cache-write-untrusted", Description: "Report cache write access granted on low-trust triggers", Category: "security", Subcategory: "cache", Recommended: true, Explanation: "Report cache write access reachable from low-trust triggers. Untrusted code or input can poison caches consumed by privileged workflows.", Help: "Set cache-mode to read or none, restrict the job with an event guard, or document a reviewed exception with an inline suppression.",
	Checks: func(c define.Inputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheWriteUntrustedChecks(workflow)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.CachePolicyEnabled("cache-write-untrusted") },
}

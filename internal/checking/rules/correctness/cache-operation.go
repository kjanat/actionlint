package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// CacheOperationRule is the single definition of the cache-operation rule.
var CacheOperationRule = &define.Rule{
	Name:        "cache-operation",
	Description: "Report cache actions blocked by explicit cache access limits",
	Category:    "correctness",
	Subcategory: "cache",
	Recommended: true,
	Explanation: "Report cache save or restore operations blocked by an explicit cache mode. GitHub skips the blocked operations.",
	Help:        "Remove the cache step or choose an action and cache mode that allow the intended operation.",
	Prepare:     define.PrepareWorkflowMetadata,
	Checks: func(c define.Inputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		return CacheOperationChecks(workflow, c.Workflows)
	},
	Enabled: func(c define.Inputs) bool { return c.Config.CachePolicyEnabled("cache-operation") },
}

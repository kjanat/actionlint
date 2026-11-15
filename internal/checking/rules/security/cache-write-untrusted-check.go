package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// CacheWriteUntrustedChecks reports write-capable cache declarations reachable
// from low-trust events. It prepares declaration ownership before callbacks run.
func CacheWriteUntrustedChecks(workflow workflowdecl.Workflow) define.Checks {
	access := cachepolicy.NewTrust(workflow)
	return define.Checks{Job: func(job workflowdecl.Job) []reporting.Finding {
		return reporting.FromDiagnostics(access.CheckWrites(job))
	},
	}
}

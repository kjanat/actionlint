package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// CacheCallUnrestrictedChecks requires an explicit cache ceiling on low-trust
// reusable calls, using the supplied workflow's triggering events.
func CacheCallUnrestrictedChecks(workflow workflowdecl.Workflow) define.Checks {
	access := cachepolicy.NewTrust(workflow)
	return define.Checks{
		Job: func(job workflowdecl.Job) []reporting.Finding {
			return reporting.FromDiagnostics(access.CheckCall(job))
		},
	}
}

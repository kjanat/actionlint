package security

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// CacheWriteUntrustedCheck reports write-capable cache declarations reachable
// from low-trust events. It prepares declaration ownership before callbacks run.
func CacheWriteUntrustedCheck(workflow workflowdecl.WorkflowView) checkinputs.DeclarationCheck {
	access := cachepolicy.NewTrust(workflow)
	return checkinputs.DeclarationCheck{
		Declaration: CacheWriteUntrustedDeclaration(),
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			return reporting.FromDiagnostics(access.CheckWrites(job))
		},
	}
}

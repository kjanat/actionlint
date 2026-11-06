package security

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// CacheCallUnrestrictedCheck requires an explicit cache ceiling on low-trust
// reusable calls, using the supplied workflow's triggering events.
func CacheCallUnrestrictedCheck(workflow workflowdecl.WorkflowView) checkinputs.DeclarationCheck {
	access := cachepolicy.NewTrust(workflow)
	return checkinputs.DeclarationCheck{
		Declaration: CacheCallUnrestrictedDeclaration(),
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			return reporting.FromDiagnostics(access.CheckCall(job))
		},
	}
}

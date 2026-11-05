package security

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleCacheCallUnrestricted adapts reusable-call cache limits to the workflow traversal.
type ruleCacheCallUnrestricted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

// NewRuleCacheCallUnrestricted requires explicit cache ceilings on low-trust reusable calls.
func NewRuleCacheCallUnrestricted() workflowvisit.Rule {
	descriptor := CacheCallUnrestrictedDeclaration()
	return &ruleCacheCallUnrestricted{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description)}
}

func (r *ruleCacheCallUnrestricted) VisitWorkflowPre(w workflowdecl.WorkflowView) error {
	r.trust = cachepolicy.NewTrust(w)
	return nil
}

func (r *ruleCacheCallUnrestricted) VisitJobPre(job workflowdecl.JobView) error {
	for _, d := range r.trust.CheckCall(job) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

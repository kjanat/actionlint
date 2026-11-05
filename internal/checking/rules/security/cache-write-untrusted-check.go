package security

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleCacheWriteUntrusted adapts cache trust checks to the workflow traversal.
type ruleCacheWriteUntrusted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

// NewRuleCacheWriteUntrusted reports write-capable cache modes on low-trust triggers.
func NewRuleCacheWriteUntrusted() workflowvisit.Rule {
	descriptor := CacheWriteUntrustedDeclaration()
	return &ruleCacheWriteUntrusted{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description)}
}

func (r *ruleCacheWriteUntrusted) VisitWorkflowPre(w workflowdecl.WorkflowView) error {
	r.trust = cachepolicy.NewTrust(w)
	return nil
}

func (r *ruleCacheWriteUntrusted) VisitJobPre(job workflowdecl.JobView) error {
	for _, d := range r.trust.CheckWrites(job) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

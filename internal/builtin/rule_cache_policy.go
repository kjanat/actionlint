package builtin

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow/cachepolicy"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// ruleCacheWriteUntrusted adapts cache trust checks to the workflow traversal.
type ruleCacheWriteUntrusted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

// NewRuleCacheWriteUntrusted reports write-capable cache modes on low-trust triggers.
func NewRuleCacheWriteUntrusted() workflowvisit.Rule {
	return &ruleCacheWriteUntrusted{Base: builtinRuleBase("cache-write-untrusted")}
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

// ruleCacheCallUnrestricted adapts reusable-call cache limits to the workflow traversal.
type ruleCacheCallUnrestricted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

// NewRuleCacheCallUnrestricted requires explicit cache ceilings on low-trust reusable calls.
func NewRuleCacheCallUnrestricted() workflowvisit.Rule {
	return &ruleCacheCallUnrestricted{Base: builtinRuleBase("cache-call-unrestricted")}
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

// ruleCacheOperation adapts effective cache access to job and step checks.
type ruleCacheOperation struct {
	workflowvisit.Base
	workflow, job workflowdecl.CacheModeView
	cache         *calls.Cache
}

// NewRuleCacheOperation reports official cache action steps disabled by an
// explicit cache mode. Without a workflow cache it does not check
// reusable-workflow calls.
func NewRuleCacheOperation() workflowvisit.Rule { return newRuleCacheOperation(nil) }

// newRuleCacheOperation also checks reusable-workflow calls when cache is not nil.
func newRuleCacheOperation(cache *calls.Cache) *ruleCacheOperation {
	return &ruleCacheOperation{Base: builtinRuleBase("cache-operation"), cache: cache}
}

func (r *ruleCacheOperation) VisitWorkflowPre(w workflowdecl.WorkflowView) error {
	r.workflow = w.CacheMode()
	return nil
}

func (r *ruleCacheOperation) VisitJobPre(job workflowdecl.JobView) error {
	r.job = cachepolicy.EffectiveMode(r.workflow, job.CacheMode())
	if r.cache != nil {
		for _, d := range cachepolicy.CheckCallOperations(job.WorkflowCall(), r.job, r.cache) {
			r.Report(findingmodel.FromDiagnostic(d))
		}
	}
	return nil
}

func (r *ruleCacheOperation) VisitStep(step workflowdecl.StepView) error {
	for _, d := range cachepolicy.CheckStep(step, r.job) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

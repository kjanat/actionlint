package correctness

import (
	cachepolicy "actionlint.kjanat.dev/v2/internal/checking/cache"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/model/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// ruleCacheOperation adapts effective cache access to job and step checks.
type ruleCacheOperation struct {
	workflowvisit.Base
	workflow, job workflowdecl.CacheModeView
	cache         *calls.Cache
}

// NewRuleCacheOperation checks step cache access and reusable calls when cache is supplied.
func NewRuleCacheOperation(cache *calls.Cache) workflowvisit.Rule {
	descriptor := CacheOperationDeclaration()
	return &ruleCacheOperation{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), cache: cache}
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

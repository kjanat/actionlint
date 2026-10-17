package builtin

import (
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/cachepolicy"
	"actionlint.kjanat.dev/internal/workflow/calls"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// RuleCacheWriteUntrusted adapts cache trust checks to the workflow traversal.
type RuleCacheWriteUntrusted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

func NewRuleCacheWriteUntrusted() *RuleCacheWriteUntrusted {
	return &RuleCacheWriteUntrusted{Base: builtinRuleBase("cache-write-untrusted")}
}
func (r *RuleCacheWriteUntrusted) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.trust = cachepolicy.NewTrust(w)
	return nil
}
func (r *RuleCacheWriteUntrusted) VisitJobPre(job *workflowlang.Job) error {
	for _, d := range r.trust.CheckWrites(job) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

// RuleCacheCallUnrestricted adapts reusable-call cache limits to the workflow traversal.
type RuleCacheCallUnrestricted struct {
	workflowvisit.Base
	trust *cachepolicy.Trust
}

func NewRuleCacheCallUnrestricted() *RuleCacheCallUnrestricted {
	return &RuleCacheCallUnrestricted{Base: builtinRuleBase("cache-call-unrestricted")}
}
func (r *RuleCacheCallUnrestricted) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.trust = cachepolicy.NewTrust(w)
	return nil
}
func (r *RuleCacheCallUnrestricted) VisitJobPre(job *workflowlang.Job) error {
	for _, d := range r.trust.CheckCall(job) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

// RuleCacheOperation adapts effective cache access to job and step checks.
type RuleCacheOperation struct {
	workflowvisit.Base
	workflow, job *workflowlang.CacheMode
	cache         *calls.Cache
}

func NewRuleCacheOperation() *RuleCacheOperation {
	return &RuleCacheOperation{Base: builtinRuleBase("cache-operation")}
}
func newRuleCacheOperation(cache *calls.Cache) *RuleCacheOperation {
	r := NewRuleCacheOperation()
	r.cache = cache
	return r
}
func (r *RuleCacheOperation) VisitWorkflowPre(w *workflowlang.Workflow) error {
	r.workflow = w.CacheMode
	return nil
}
func (r *RuleCacheOperation) VisitJobPre(job *workflowlang.Job) error {
	r.job = workflowlang.EffectiveCacheMode(r.workflow, job.CacheMode)
	if r.cache != nil {
		for _, d := range cachepolicy.CheckCallOperations(job.WorkflowCall, r.job, r.cache) {
			r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
		}
	}
	return nil
}
func (r *RuleCacheOperation) VisitStep(step *workflowlang.Step) error {
	for _, d := range cachepolicy.CheckStep(step, r.job) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

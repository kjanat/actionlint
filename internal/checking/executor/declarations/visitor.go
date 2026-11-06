package declarations

import (
	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// declarationRule is the traversal adapter for stateless declaration checks.
type declarationRule struct {
	workflowvisit.Base
	workflowCheck func(workflowdecl.WorkflowView) []reporting.Finding
	jobCheck      func(workflowdecl.JobView) []reporting.Finding
	stepCheck     func(workflowdecl.StepView) []reporting.Finding
	rules         repoconfig.RuleSet
}

// invocationDeclarationRule applies a source-local check to composite bodies.
// Workflow entry steps use the ordinary visitor and are not checked twice.
type invocationDeclarationRule struct {
	*declarationRule
	contexts []invocation.Context
}

func (r *invocationDeclarationRule) EnterInvocation(scope checkcalls.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.contexts = append(r.contexts, scope.Context)
	return nil
}

func (r *invocationDeclarationRule) LeaveInvocation(scope invocation.Context) error {
	if len(r.contexts) == 0 || r.contexts[len(r.contexts)-1] != scope {
		return invocation.ErrForeignContext
	}
	r.contexts = r.contexts[:len(r.contexts)-1]
	return nil
}

func (r *invocationDeclarationRule) VisitInvocationStep(step workflowdecl.StepView) error {
	return r.VisitStep(step)
}

func (r *invocationDeclarationRule) VisitOpaqueAction(step workflowdecl.StepView) error {
	if len(r.contexts) == 0 {
		return nil
	}
	return r.VisitStep(step)
}

// New invokes only the workflow, job and step checks supplied by the rule.
func New(rules repoconfig.RuleSet, check checkinputs.DeclarationCheck) *declarationRule {
	if check.Workflow == nil && check.Job == nil && check.Step == nil {
		panic("rule " + check.Name + " supplies no declaration check")
	}
	metadata, found := rules.FindRule(check.Name)
	if !found {
		panic("unregistered rule: " + check.Name)
	}
	return &declarationRule{
		Base: workflowvisit.NewBase(metadata.Name, metadata.Description), rules: rules,
		workflowCheck: check.Workflow, jobCheck: check.Job, stepCheck: check.Step,
	}
}

func (r *declarationRule) collect(input []reporting.Finding, job string) {
	for _, d := range input {
		finding := findingmodel.FromDiagnostic(d.Diagnostic)
		if descriptor, known := r.rules.FindRule(finding.Rule); known {
			finding.Rule = descriptor.Name
		}
		if d.Step.Present() {
			finding.Owner = &findingmodel.Owner{Job: job, Step: reporting.SourcePosition(d.Step)}
			origin := d.Step.Origin(job)
			finding.IgnoreOrigin = &findingmodel.IgnoreOrigin{Job: origin.Job, StepID: origin.StepID, StepName: origin.StepName, Uses: origin.Uses}
			if origin.HasPosition {
				finding.IgnoreOrigin.StepPosition = &origin.Position
			}
		}
		r.Report(finding)
	}
}

func (r *declarationRule) VisitWorkflowPre(w workflowdecl.WorkflowView) error {
	if r.workflowCheck != nil {
		r.collect(r.workflowCheck(w), "")
	}
	return nil
}

func (r *declarationRule) VisitJobPre(j workflowdecl.JobView) error {
	if r.jobCheck != nil {
		r.collect(r.jobCheck(j), declarationID(j.ID()))
	}
	return nil
}

func (r *declarationRule) VisitStep(s workflowdecl.StepView) error {
	if r.stepCheck != nil {
		r.collect(r.stepCheck(s), "")
	}
	return nil
}

// NewVendorLockIn checks URL-form references in jobs, steps and called composites.
func NewVendorLockIn(rules repoconfig.RuleSet, runners *runnerlang.Knowledge) workflowvisit.Rule {
	r := New(rules, correctness.VendorLockInCheck(runners.Target()))
	return &invocationDeclarationRule{declarationRule: r}
}

func NewRuleRequireJobTimeout(rules repoconfig.RuleSet, policy *repoconfig.JobTimeoutPolicy) workflowvisit.Rule {
	options := conventions.TimeoutPolicy{Enabled: policy.Enabled()}
	if lower, ok := policy.MinMinutes(); ok {
		options.Min = new(lower)
	}
	if upper, ok := policy.MaxMinutes(); ok {
		options.Max = new(upper)
	}
	return New(rules, conventions.JobTimeoutCheck(options))
}

func NewRuleRequirePermissions(rules repoconfig.RuleSet, policy *repoconfig.PermissionsPolicy) workflowvisit.Rule {
	return New(rules, security.ExplicitPermissionsCheck(policy.Scope()))
}

func NewRuleRequireCommitHash(rules repoconfig.RuleSet, excluded func(string) bool) workflowvisit.Rule {
	return New(rules, security.UnpinnedReferenceCheck(excluded))
}

func declarationID(value workflowdecl.StringView) string {
	return value.Value()
}

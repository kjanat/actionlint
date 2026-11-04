package builtin

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/checks"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/rule"
)

// declarationRule is the traversal adapter for stateless declaration checks.
type declarationRule struct {
	workflowvisit.Base
	check         checks.Declaration
	workflowCheck func(workflowdecl.WorkflowView) []checks.Finding
	jobCheck      func(workflowdecl.JobView) []checks.Finding
	stepCheck     func(workflowdecl.StepView) []checks.Finding
}

// invocationDeclarationRule applies a source-local check to composite bodies.
// Workflow entry steps use the ordinary visitor and are not checked twice.
type invocationDeclarationRule struct {
	*declarationRule
	contexts []invocation.Context
}

func (r *invocationDeclarationRule) EnterInvocation(scope Invocation) error {
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

func newDeclarationRule(name string) *declarationRule {
	return &declarationRule{Base: builtinRuleBase(name), check: checks.Declaration(name)}
}

func (r *declarationRule) collect(input []checks.Finding, job string) {
	for _, d := range input {
		finding := findingmodel.FromDiagnostic(d.Diagnostic)
		finding.Rule = rule.CanonicalName(finding.Rule)
		if d.Step.Present() {
			finding.Owner = &findingmodel.Owner{Job: job, Step: declarationPosition(d.Step)}
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
	} else {
		r.collect(r.check.Workflow(w), "")
	}
	return nil
}

func (r *declarationRule) VisitJobPre(j workflowdecl.JobView) error {
	if r.jobCheck != nil {
		r.collect(r.jobCheck(j), declarationID(j.ID()))
	} else {
		r.collect(r.check.Job(j), declarationID(j.ID()))
	}
	return nil
}

func (r *declarationRule) VisitStep(s workflowdecl.StepView) error {
	if r.stepCheck != nil {
		r.collect(r.stepCheck(s), "")
	} else {
		r.collect(r.check.Step(s), "")
	}
	return nil
}

// The NewRule functions below build the declaration checks that need no host
// lookups. Each returns the catalogue rule of the same name.

func NewRuleEnvVar() workflowvisit.Rule      { return newDeclarationRule("env-var") }
func NewRuleID() workflowvisit.Rule          { return newDeclarationRule("id") }
func NewRuleJobNeeds() workflowvisit.Rule    { return newDeclarationRule("job-needs") }
func NewRulePermissions() workflowvisit.Rule { return newDeclarationRule("permissions") }
func NewRuleIfCond() workflowvisit.Rule      { return newDeclarationRule("if-cond") }

func NewRuleEmptyTriggers() workflowvisit.Rule { return newDeclarationRule("empty-triggers") }
func NewRuleEmptyJob() workflowvisit.Rule      { return newDeclarationRule("empty-job") }
func NewRuleEmptyRun() workflowvisit.Rule      { return newDeclarationRule("empty-run") }
func NewRuleNumericValues() workflowvisit.Rule { return newDeclarationRule("numeric-values") }

func newRuleVendorLockIn(runners *runnerlang.Knowledge) workflowvisit.Rule {
	r := newDeclarationRule("vendor-lock-in")
	target := runners.Target()
	r.jobCheck = func(job workflowdecl.JobView) []checks.Finding {
		return checks.VendorLockIn(job.WorkflowCall().Uses(), target)
	}
	r.stepCheck = func(step workflowdecl.StepView) []checks.Finding {
		if action, present := step.Action(); present {
			findings := checks.VendorLockIn(action.Uses(), target)
			for i := range findings {
				findings[i].Step = step
			}
			return findings
		}
		return nil
	}
	return &invocationDeclarationRule{declarationRule: r}
}

func NewRuleCredentials() workflowvisit.Rule   { return newDeclarationRule("credentials") }
func NewRuleEvents() workflowvisit.Rule        { return newDeclarationRule("events") }
func NewRuleGlob() workflowvisit.Rule          { return newDeclarationRule("glob") }
func NewRuleParallelSteps() workflowvisit.Rule { return newDeclarationRule("parallel-steps") }

func NewRuleRequireJobTimeout(policy *repoconfig.JobTimeoutPolicy) workflowvisit.Rule {
	options := checks.TimeoutPolicy{Enabled: policy.Enabled()}
	if lower, ok := policy.MinMinutes(); ok {
		options.Min = new(lower)
	}
	if upper, ok := policy.MaxMinutes(); ok {
		options.Max = new(upper)
	}
	r := newDeclarationRule("job-timeout")
	r.jobCheck = func(j workflowdecl.JobView) []checks.Finding { return checks.JobTimeout(j, options) }
	return r
}

func NewRuleRequirePermissions(policy *repoconfig.PermissionsPolicy) workflowvisit.Rule {
	scope := policy.Scope()
	r := newDeclarationRule("explicit-permissions")
	r.workflowCheck = func(w workflowdecl.WorkflowView) []checks.Finding {
		return checks.RequiredWorkflowPermissions(w, scope)
	}
	r.jobCheck = func(j workflowdecl.JobView) []checks.Finding { return checks.RequiredJobPermissions(j, scope) }
	return r
}

func NewRuleRequireCommitHash(excluded func(string) bool) workflowvisit.Rule {
	r := newDeclarationRule("unpinned-reference")
	r.jobCheck = func(j workflowdecl.JobView) []checks.Finding { return checks.CommitHashJob(j, excluded) }
	r.stepCheck = func(s workflowdecl.StepView) []checks.Finding { return checks.CommitHashStep(s, excluded) }
	return r
}

func declarationID(value workflowdecl.StringView) string {
	return value.Value()
}

func declarationPosition(view interface{ Pos() (workflow.Pos, bool) }) *workflow.Pos {
	position, present := view.Pos()
	if !present {
		return nil
	}
	return &position
}

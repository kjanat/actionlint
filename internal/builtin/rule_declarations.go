package builtin

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/checks"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// declarationRule is the traversal adapter for stateless declaration checks.
type declarationRule struct {
	workflowvisit.Base
	check         checks.Declaration
	workflowCheck func(workflowdecl.WorkflowView) []checks.Finding
	jobCheck      func(workflowdecl.JobView) []checks.Finding
	stepCheck     func(workflowdecl.StepView) []checks.Finding
}

func newDeclarationRule(name string) *declarationRule {
	return &declarationRule{Base: builtinRuleBase(name), check: checks.Declaration(name)}
}

func (r *declarationRule) collect(input []checks.Finding, job string) {
	for _, d := range input {
		finding := findingmodel.FromDiagnostic(d.Diagnostic)
		if d.Step.Present() {
			finding.Owner = &findingmodel.Owner{Job: job, Step: declarationPosition(d.Step)}
			origin := d.Step.Origin(job)
			finding.IgnoreOrigin = &findingmodel.IgnoreOrigin{Job: origin.Job, StepID: origin.StepID, StepName: origin.StepName, Uses: origin.Uses}
			if origin.HasPosition {
				finding.IgnoreOrigin.StepPosition = &origin.Position
			}
		}
		r.Findings = append(r.Findings, finding)
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

func NewRuleEnvVar() *declarationRule      { return newDeclarationRule("env-var") }
func NewRuleID() *declarationRule          { return newDeclarationRule("id") }
func NewRuleJobNeeds() *declarationRule    { return newDeclarationRule("job-needs") }
func NewRulePermissions() *declarationRule { return newDeclarationRule("permissions") }
func NewRuleIfCond() *declarationRule      { return newDeclarationRule("if-cond") }

func NewRuleCredentials() *declarationRule   { return newDeclarationRule("credentials") }
func NewRuleEvents() *declarationRule        { return newDeclarationRule("events") }
func NewRuleGlob() *declarationRule          { return newDeclarationRule("glob") }
func NewRuleParallelSteps() *declarationRule { return newDeclarationRule("parallel-steps") }

func NewRuleRequireJobTimeout(policy *repoconfig.JobTimeoutPolicy) *declarationRule {
	options := checks.TimeoutPolicy{Enabled: policy.Enabled()}
	if lower, ok := policy.MinMinutes(); ok {
		options.Min = new(lower)
	}
	if upper, ok := policy.MaxMinutes(); ok {
		options.Max = new(upper)
	}
	r := newDeclarationRule("require-job-timeout")
	r.jobCheck = func(j workflowdecl.JobView) []checks.Finding { return checks.JobTimeout(j, options) }
	return r
}

func NewRuleRequirePermissions(policy *repoconfig.PermissionsPolicy) *declarationRule {
	scope := policy.Scope()
	r := newDeclarationRule("require-permissions")
	r.workflowCheck = func(w workflowdecl.WorkflowView) []checks.Finding {
		return checks.RequiredWorkflowPermissions(w, scope)
	}
	r.jobCheck = func(j workflowdecl.JobView) []checks.Finding { return checks.RequiredJobPermissions(j, scope) }
	return r
}

func NewRuleRequiredActions(required []string) *declarationRule {
	r := newDeclarationRule("required-actions")
	r.workflowCheck = func(w workflowdecl.WorkflowView) []checks.Finding { return checks.RequiredActions(w, required) }
	return r
}

func NewRuleRequireCommitHash(excluded func(string) bool) *declarationRule {
	r := newDeclarationRule("require-commit-hash")
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

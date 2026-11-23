package define

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Checks are stateless checks of published workflow views. A nil callback
// requests no input of that kind. Each callback returns independent findings
// and may run without any preceding or following callback. Findings may leave
// Rule empty; the adapter attaches the rule's name.
type Checks struct {
	// WorkflowStepsOnly keeps step traversal in workflows even when a rule accepts action diagnostics.
	WorkflowStepsOnly bool
	Workflow          func(workflows.BoundWorkflow) []reporting.StepFinding
	Job               func(workflows.BoundJob) []reporting.StepFinding
	Step              func(workflows.BoundStep) []reporting.StepFinding
}

type checksVisitor struct {
	workflowvisit.Base
	name   string
	checks Checks
	rules  configuration.RuleSet
}

// Workflow entry steps use the ordinary visitor instead of invocationChecksVisitor.
type invocationChecksVisitor struct {
	*checksVisitor
	contexts []invocation.Context
}

func (r *invocationChecksVisitor) EnterInvocation(scope workflowvisit.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	r.contexts = append(r.contexts, scope.Context)
	return nil
}

func (r *invocationChecksVisitor) LeaveInvocation(scope invocation.Context) error {
	if len(r.contexts) == 0 || r.contexts[len(r.contexts)-1] != scope {
		return invocation.ErrForeignContext
	}
	r.contexts = r.contexts[:len(r.contexts)-1]
	return nil
}

func (r *invocationChecksVisitor) VisitInvocationStep(step workflows.BoundStep) error {
	return r.VisitStep(step)
}

func (r *invocationChecksVisitor) VisitOpaqueAction(step workflows.BoundStep) error {
	if len(r.contexts) == 0 {
		return nil
	}
	return r.VisitStep(step)
}

// NewChecksVisitor invokes only the workflow, job and step checks supplied by
// the named rule. The rule must be known to rules.
func NewChecksVisitor(rules configuration.RuleSet, name string, checks Checks) workflowvisit.Rule {
	if checks.Workflow == nil && checks.Job == nil && checks.Step == nil {
		panic("rule " + name + " supplies no declaration check")
	}
	metadata, found := rules.FindRule(name)
	if !found {
		panic("unregistered rule: " + name)
	}
	adapter := &checksVisitor{Base: workflowvisit.NewBase(metadata.Name, metadata.Description), name: metadata.Name, rules: rules, checks: checks}
	if metadata.SupportsActions && !checks.WorkflowStepsOnly {
		return &invocationChecksVisitor{checksVisitor: adapter}
	}
	return adapter
}

func (r *checksVisitor) collect(input []reporting.StepFinding, job string) {
	for _, d := range input {
		item := new(d.Finding)
		if item.Rule == "" {
			item.Rule = r.name
		} else if descriptor, known := r.rules.FindRule(item.Rule); known {
			item.Rule = descriptor.Name
		}
		if d.Step.Present() {
			item.Owner = &finding.Owner{Job: job, Step: sourcetext.NilUnlessPresent(d.Step.Position())}
			item.IgnoreOrigin = d.Step.IgnoreOrigin(job)
		}
		r.Report(item)
	}
}

func (r *checksVisitor) VisitWorkflowPre(w workflows.BoundWorkflow) error {
	if r.checks.Workflow != nil {
		r.collect(r.checks.Workflow(w), "")
	}
	return nil
}

func (r *checksVisitor) VisitJobPre(j workflows.BoundJob) error {
	if r.checks.Job != nil {
		r.collect(r.checks.Job(j), j.ID.Text())
	}
	return nil
}

func (r *checksVisitor) VisitStep(s workflows.BoundStep) error {
	if r.checks.Step != nil {
		r.collect(r.checks.Step(s), s.DeclaringJob())
	}
	return nil
}

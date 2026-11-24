package correctness

import (
	"context"
	"errors"
	"os"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

var ExecutableBitRule = &define.Rule{
	Name:        "executable-bit",
	Description: "Report directly invoked repository scripts not marked executable in Git",
	Category:    "correctness",
	Subcategory: "scripts",
	Recommended: true,
	Help:        "Set the executable bit in Git, or run the script through its interpreter.",
	Prepare:     prepareExecutableIndex,
	Walker: func(c define.RuleInputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewRuleExecutableBit(base, ExecutableContext{
			ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
			Index: c.ExecutableIndex, Runners: c.Runners,
		})
	},
	Enabled: func(c define.RuleInputs) bool { return c.ProjectRoot != "" && c.Index != nil },
}

type ruleExecutableBit struct {
	*ExecutableChecker
	reporting.Logger
	name, description string
	frames            []executableInvocation
	findings          workflowvisit.Findings
}

func NewRuleExecutableBit(base workflowvisit.Base, input ExecutableContext) workflowvisit.Rule {
	r := &ruleExecutableBit{Tag: base.Name(), name: base.Name(), description: base.Description()}
	input.Report = func(finding *finding.Finding) { r.findings.Report(finding) }
	r.ExecutableChecker = NewExecutableChecker(input)
	return r
}
func (rule *ruleExecutableBit) Name() string        { return rule.name }
func (rule *ruleExecutableBit) Description() string { return rule.description }

func (rule *ruleExecutableBit) Errs() []*finding.Finding { return rule.findings.Errs() }
func (rule *ruleExecutableBit) FindingCount() int        { return rule.findings.FindingCount() }

func (rule *ruleExecutableBit) AnnotateSince(mark int, annotate func(*finding.Finding)) {
	rule.findings.AnnotateSince(mark, annotate)
}
func (rule *ruleExecutableBit) VisitWorkflowPost(workflows.BoundWorkflow) error { return nil }
func (rule *ruleExecutableBit) VisitJobPost(workflows.BoundJob) error           { return nil }
func (rule *ruleExecutableBit) ApplyCheckout(actions *localactions.Resolver) {
	rule.SetPaths(actions.ApplyCheckout(rule.Paths()))
}

type executableInvocation struct {
	scope  workflowvisit.Invocation
	parent *ExecutableChecker
}

func (rule *ruleExecutableBit) VisitStep(step workflows.BoundStep) error {
	if _, action := step.Action(); action {
		return nil
	}
	return rule.VisitInvocationStep(step)
}

func (rule *ruleExecutableBit) VisitInvocationStep(step workflows.BoundStep) error {
	return rule.ExecutableChecker.VisitStep(step)
}

func (rule *ruleExecutableBit) VisitOpaqueAction(step workflows.BoundStep) error {
	return rule.VisitInvocationStep(step)
}

func (rule *ruleExecutableBit) EnterInvocation(scope workflowvisit.Invocation) error {
	if !scope.Context.Valid() || !scope.Source.Present() {
		return invocation.ErrForeignContext
	}
	rule.frames = append(rule.frames, executableInvocation{scope: scope, parent: rule.ExecutableChecker})
	rule.ExecutableChecker = rule.ForkComposite(scope.Context, scope.ActionPath, rule.Paths())
	return nil
}

func (rule *ruleExecutableBit) LeaveInvocation(context invocation.Context) error {
	if len(rule.frames) == 0 || rule.frames[len(rule.frames)-1].scope.Context != context {
		return invocation.ErrForeignContext
	}
	frame := rule.frames[len(rule.frames)-1]
	frame.parent.JoinComposite(context, rule.ExecutableChecker)
	rule.ExecutableChecker = frame.parent
	rule.frames = rule.frames[:len(rule.frames)-1]
	return nil
}

var _ workflowvisit.InvocationParticipant = (*ruleExecutableBit)(nil)

func prepareExecutableIndex(c define.RuleInputs) (define.InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, "executable-bit") == "off" {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	if err := c.Context.Err(); err != nil {
		return nil, err
	}
	snapshot := c.Index(c.Context, c.ProjectRoot)
	if err := c.Context.Err(); err != nil {
		return nil, err
	}
	if err := snapshot.Err(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		return define.UnavailableInputs{Reason: "Git index could not be read: " + err.Error()}, nil
	}
	c.Reader.Record(snapshot.Path())
	c.ExecutableIndex = snapshot
	return define.PreparedInputs{RuleInputs: c}, nil
}

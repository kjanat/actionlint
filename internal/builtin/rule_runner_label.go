package builtin

import (
	"errors"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type ruleRunnerLabel struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	configuration
}

// NewRuleRunnerLabel checks known runner compatibility and available label facts.
func NewRuleRunnerLabel() workflowvisit.Rule { return newRuleRunnerLabel(nil) }

// newRuleRunnerLabel reports the job findings of runners instead when runners
// is not nil.
func newRuleRunnerLabel(runners *runnerlang.Knowledge) *ruleRunnerLabel {
	return &ruleRunnerLabel{Base: builtinRuleBase("runner-label"), runners: runners}
}

func (r *ruleRunnerLabel) VisitJobPre(declaration workflowdecl.JobView) error {
	if !declaration.Present() {
		return errors.New("runner-label job declaration is unavailable")
	}
	if r.runners != nil {
		for _, d := range r.runners.Job(declaration).Findings {
			r.Report(findingmodel.FromDiagnostic(d))
		}
		return nil
	}
	labels := runnerlang.LabelSet{}
	if known, available := r.repositoryFacts().RunnerLabels.Value(); available {
		labels = runnerlang.KnownLabels(known.Values())
	}
	for _, d := range runnerlang.CheckWithLabels(declaration, labels) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

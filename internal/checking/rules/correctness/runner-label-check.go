package correctness

import (
	"errors"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type ruleRunnerLabel struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	labels runnerlang.LabelSet
}

// NewRuleRunnerLabel checks runner knowledge or the supplied repository labels.
func NewRuleRunnerLabel(runners *runnerlang.Knowledge, labels runnerlang.LabelSet) workflowvisit.Rule {
	descriptor := RunnerLabelDeclaration()
	return &ruleRunnerLabel{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), runners: runners, labels: labels}
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
	for _, d := range runnerlang.CheckWithLabels(declaration, r.labels) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

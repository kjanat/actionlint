package correctness

import (
	"errors"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"
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
	var rejections []runnerapi.Rejection
	if r.runners != nil {
		rejections = r.runners.Job(declaration).Rejections
	} else {
		rejections = runnerlang.LabelRejections(declaration, r.labels)
	}
	for _, rejection := range rejections {
		r.Report(findingmodel.FromDiagnostic(results.Diagnostic{Rule: "runner-label", Start: rejection.Position, Message: rejection.Reason}))
	}
	return nil
}

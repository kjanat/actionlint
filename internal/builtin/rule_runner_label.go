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

func NewRuleRunnerLabel() *ruleRunnerLabel {
	return &ruleRunnerLabel{Base: builtinRuleBase("runner-label")}
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
	var labels []string
	if r.config != nil {
		labels = r.config.SelfHostedRunner.Labels
	}
	for _, d := range runnerlang.Check(declaration, labels) {
		r.Report(findingmodel.FromDiagnostic(d))
	}
	return nil
}

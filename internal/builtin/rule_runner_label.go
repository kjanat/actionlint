package builtin

import (
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type RuleRunnerLabel struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	Configuration
}

func NewRuleRunnerLabel() *RuleRunnerLabel {
	return &RuleRunnerLabel{Base: builtinRuleBase("runner-label")}
}

func (r *RuleRunnerLabel) VisitJobPre(job *workflowlang.Job) error {
	if r.runners != nil {
		for _, d := range r.runners.Job(job).Findings {
			r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
		}
		return nil
	}
	var labels []string
	if r.config != nil {
		labels = r.config.SelfHostedRunner.Labels
	}
	for _, d := range runnerlang.Check(job, labels) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

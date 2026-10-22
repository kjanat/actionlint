package builtin

import (
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

type RuleRunnerLabel struct {
	workflowvisit.Base
	Configuration
}

func NewRuleRunnerLabel() *RuleRunnerLabel {
	return &RuleRunnerLabel{Base: builtinRuleBase("runner-label")}
}

func (r *RuleRunnerLabel) VisitJobPre(job *workflowlang.Job) error {
	var labels []string
	if r.config != nil {
		labels = r.config.SelfHostedRunner.Labels
	}
	for _, d := range runnerlang.Check(job, labels) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

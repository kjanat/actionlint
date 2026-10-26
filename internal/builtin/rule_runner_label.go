package builtin

import (
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"errors"
)

type RuleRunnerLabel struct {
	runners *runnerlang.Knowledge
	workflowvisit.Base
	Configuration
	source *documentparse.SourceSnapshot
}

func NewRuleRunnerLabel(source *documentparse.SourceSnapshot) *RuleRunnerLabel {
	return &RuleRunnerLabel{Base: builtinRuleBase("runner-label"), source: source}
}

func (r *RuleRunnerLabel) VisitJobPre(job *workflowlang.Job) error {
	if r.source == nil {
		return errors.New("runner-label source snapshot is unavailable")
	}
	declaration, err := r.source.JobView(job)
	if err != nil {
		return err
	}
	if r.runners != nil {
		for _, d := range r.runners.Job(declaration).Findings {
			r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
		}
		return nil
	}
	var labels []string
	if r.config != nil {
		labels = r.config.SelfHostedRunner.Labels
	}
	for _, d := range runnerlang.Check(declaration, labels) {
		r.Findings = append(r.Findings, findingmodel.FromDiagnostic(d))
	}
	return nil
}

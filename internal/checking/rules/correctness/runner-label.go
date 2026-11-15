package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// RunnerLabelRule is the single definition of the runner-label rule.
var RunnerLabelRule = &define.Rule{
	Name:         "runner-label",
	Description:  "Report unknown or incompatible runner labels",
	Category:     "correctness",
	Subcategory:  "runners",
	MovedOptions: []rule.MovedOption{{Option: "labels", Repository: "runners"}},
	Recommended:  true,
	Help:         "Correct the runner label or declare repository-specific labels in repository.runners.",
	Checks: func(c define.Inputs) define.Checks {
		labels := runnerlang.LabelSet{}
		if known, available := configuration.RepositoryFacts(c.Config, c.Repository).RunnerLabels.Value(); available {
			labels = runnerlang.KnownLabels(known.Values())
		}
		return RunnerLabelChecks(c.Runners, labels)
	},
}

// RunnerLabelChecks reports prepared runner rejections. Without prepared knowledge,
// it checks labels against the supplied repository label set.
func RunnerLabelChecks(runners *runnerlang.Knowledge, labels runnerlang.LabelSet) define.Checks {
	return define.Checks{
		Job: func(job workflowdecl.Job) []reporting.Finding {
			var rejections []runnerapi.Rejection
			if runners != nil {
				rejections = runners.Job(job).Rejections
			} else {
				rejections = runnerlang.LabelRejections(job, labels)
			}
			findings := make([]reporting.Finding, 0, len(rejections))
			for _, rejection := range rejections {
				findings = append(findings, reporting.Finding{Start: rejection.Position, Message: rejection.Reason})
			}
			return findings
		},
	}
}

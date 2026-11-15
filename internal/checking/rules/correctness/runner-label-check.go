package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

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

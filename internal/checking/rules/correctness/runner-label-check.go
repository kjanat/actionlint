package correctness

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// RunnerLabelCheck reports prepared runner rejections. Without prepared knowledge,
// it checks labels against the supplied repository label set.
func RunnerLabelCheck(runners *runnerlang.Knowledge, labels runnerlang.LabelSet) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: RunnerLabelDeclaration(),
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			var rejections []runnerapi.Rejection
			if runners != nil {
				rejections = runners.Job(job).Rejections
			} else {
				rejections = runnerlang.LabelRejections(job, labels)
			}
			findings := make([]reporting.Finding, 0, len(rejections))
			for _, rejection := range rejections {
				findings = append(findings, reporting.Finding{Rule: "runner-label", Start: rejection.Position, Message: rejection.Reason})
			}
			return findings
		},
	}
}

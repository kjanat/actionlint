package correctness

import (
	"fmt"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// RunnerSupportCheck reports support facts without changing which runners match.
func RunnerSupportCheck(runners *runnerlang.Knowledge) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: RunnerSupportDeclaration(),
		Job: func(job workflowdecl.JobView) []reporting.Finding {
			var findings []reporting.Finding
			for _, problem := range runners.Support(job, runnerapi.JobExecution) {
				position := supportPosition(job, problem.Selector)
				message := supportMessage(problem)
				finding := reporting.Finding{Rule: "runner-support", Code: string(problem.State), Severity: "warning", Message: message, Start: position, DocumentationURL: problem.Policy.URL}
				if !slices.ContainsFunc(findings, func(existing reporting.Finding) bool {
					return existing.Start == finding.Start && existing.Message == finding.Message
				}) {
					findings = append(findings, finding)
				}
			}
			return findings
		},
	}
}

func supportPosition(job workflowdecl.JobView, selector runnerapi.Selector) sourcepos.Position {
	for _, label := range selector.Labels {
		if label.Position.Line > 0 {
			return label.Position
		}
	}
	if selector.Group != nil && selector.Group.Position.Line > 0 {
		return selector.Group.Position
	}
	position, _ := job.Pos()
	return sourcepos.Position{Line: position.Line, Column: position.Col}
}

func supportMessage(problem runnerlang.SupportProblem) string {
	if problem.State == runnerapi.Deprecated {
		if problem.Next != nil && problem.Next.State == runnerapi.Unsupported {
			return fmt.Sprintf("Support for %s will end by %s.", problem.Policy.Description, problem.Next.On)
		}
		return fmt.Sprintf("Support for %s is deprecated.", problem.Policy.Description)
	}
	if problem.Next != nil && problem.Next.State == runnerapi.Supported {
		return fmt.Sprintf("Support for %s is scheduled to start on %s.", problem.Policy.Description, problem.Next.On)
	}
	if !problem.Effective.IsZero() {
		return fmt.Sprintf("Support for %s has ended as of %s.", problem.Policy.Description, problem.Effective)
	}
	return problem.Policy.Description + " is not supported."
}

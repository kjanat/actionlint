package correctness

import (
	"fmt"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
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
			problems := append(runners.Support(job, runnerapi.JobExecution), runners.UpcomingSupport(job, runnerapi.JobExecution)...)
			for _, problem := range problems {
				position := supportPosition(job, problem.Selector)
				message := supportMessage(problem)
				finding := reporting.Finding{Rule: "runner-support", Code: string(problem.State), Severity: "warning", Message: message, Start: position, DocumentationURL: problem.Policy.URL, LifecycleEvidence: problem.Evidence}
				if problem.Evidence != nil && !problem.Evidence.Current {
					finding.Code = string(problem.Evidence.Kind)
				}
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
	if problem.Evidence != nil && !problem.Evidence.Current {
		if problem.Evidence.Kind == lifecycle.SupportStarts {
			return fmt.Sprintf("Support for %s is scheduled to start on %s.", problem.Policy.Description, problem.Evidence.Effective)
		}
		return fmt.Sprintf("Support for %s will end by %s.", problem.Policy.Description, problem.Evidence.Effective)
	}
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

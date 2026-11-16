package correctness

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// RunnerSupportRule is the single definition of the runner-support rule.
var RunnerSupportRule = &define.Rule{
	Name:         "runner-support",
	Description:  "Report runner selections with deprecated or unsupported execution",
	Category:     "correctness",
	Subcategory:  "runners",
	Recommended:  true,
	DefaultLevel: "warn",
	Explanation:  "Report dated support changes established by the selected runner provider for the workflow's execution service. GitHub.com support dates do not apply to GitHub Enterprise Server. Hosted labels such as ubuntu-24.04-arm select ARM64 runners and remain valid. Unknown, stale or incomplete evidence does not establish a support problem.",
	Help:         "Select a supported runner architecture or update the runner inventory. Set repository.service to the service that executes the workflow, and supply the GHES version when known.",
	Checks:       func(c define.Inputs) define.Checks { return RunnerSupportChecks(c.Runners) },
}

// RunnerSupportChecks reports support facts without changing which runners match.
func RunnerSupportChecks(runners *runnerlang.Knowledge) define.Checks {
	return define.Checks{
		Job: func(job workflowdecl.Job) []reporting.StepFinding {
			var findings []reporting.StepFinding
			problems := append(runners.Support(job, runnerapi.JobExecution), runners.UpcomingSupport(job, runnerapi.JobExecution)...)
			for _, problem := range problems {
				position := supportPosition(job, problem.Selector)
				message := supportMessage(problem)
				finding := reporting.StepFinding{Code: string(problem.State), Severity: "warning", Message: message, Start: position, DocumentationURL: problem.Policy.URL, LifecycleEvidence: problem.Evidence}
				if problem.Evidence != nil && !problem.Evidence.Current {
					finding.Code = string(problem.Evidence.Kind)
				}
				if !slices.ContainsFunc(findings, func(existing reporting.StepFinding) bool {
					return existing.Start == finding.Start && existing.Message == finding.Message
				}) {
					findings = append(findings, finding)
				}
			}
			return findings
		},
	}
}

func supportPosition(job workflowdecl.Job, selector runnerapi.Selector) sourcepos.Position {
	for _, label := range selector.Labels {
		if label.Position.Line > 0 {
			return label.Position
		}
	}
	if selector.Group != nil && selector.Group.Position.Line > 0 {
		return selector.Group.Position
	}
	position, _ := job.Position()
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

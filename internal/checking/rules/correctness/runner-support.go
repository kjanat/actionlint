package correctness

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var RunnerSupportRule = &define.Rule{
	Name:         "runner-support",
	Description:  "Report runner selections with deprecated or unsupported execution",
	Category:     "correctness",
	Subcategory:  "runners",
	Recommended:  true,
	DefaultLevel: "warn",
	Help:         "Set repository.service to the service that runs the workflow, with the GHES version if known.",
	Checks:       func(c define.RuleInputs) define.Checks { return RunnerSupportChecks(c.Runners) },
}

func RunnerSupportChecks(runners *runners.Knowledge) define.Checks {
	return define.Checks{
		Job: func(job workflows.BoundJob) []reporting.StepFinding {
			var findings []reporting.StepFinding
			problems := append(runners.Support(job, runnerapi.JobExecution), runners.UpcomingSupport(job, runnerapi.JobExecution)...)
			for _, problem := range problems {
				position := supportPosition(job, problem.Selector)
				message := supportMessage(problem)
				finding := reporting.StepFinding{Code: string(problem.State), Severity: severity.Warning, Message: message, Start: position, DocumentationURL: problem.Policy.URL, LifecycleEvidence: problem.Evidence}
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

func supportPosition(job workflows.BoundJob, selector runnerapi.Selector) sourcetext.Position {
	for _, label := range selector.Labels {
		if label.Position.Line > 0 {
			return label.Position
		}
	}
	if selector.Group != nil && selector.Group.Position.Line > 0 {
		return selector.Group.Position
	}
	position, _ := job.Position()
	return position
}

func supportMessage(problem runners.SupportProblem) string {
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

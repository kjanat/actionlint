package security

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

var MissingPermissionsRule = &define.Rule{
	Name:         "missing-permissions",
	Description:  "Report missing token permission declarations",
	Category:     "security",
	Subcategory:  "permissions",
	Recommended:  true,
	DefaultLevel: "error",
	Explanation:  "Report jobs with no permissions on the job or workflow.",
	Help:         "Add permissions to the workflow or every job, or to each location that require-at lists. Use permissions: {} to grant none.",
	Options:      func() any { return &configuration.MissingPermissionsOptions{} },
	Examples:     []rule.Example{{Before: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", After: "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"}},
	Prepare:      preparePermissionRequirements,
	Checks: func(c define.RuleInputs) define.Checks {
		return MissingPermissionsChecks(c.RequiredPermissionLocations)
	},
}

func MissingPermissionsChecks(locations []configuration.PermissionLocation) define.Checks {
	locations = slices.Clone(locations)
	return define.Checks{Workflow: func(workflow workflows.BoundWorkflow) []reporting.StepFinding {
		return MissingPermissions(workflow, locations)
	}}
}

func MissingPermissions(workflow workflows.BoundWorkflow, locations []configuration.PermissionLocation) []reporting.StepFinding {
	defaultLocations := len(locations) == 0
	requireWorkflow := slices.Contains(locations, configuration.WorkflowPermissions)
	requireJob := slices.Contains(locations, configuration.EveryJobPermissions)
	if !workflow.Present() || defaultLocations && workflow.Permissions != nil {
		return nil
	}
	findings := reporting.Collector{}
	if requireWorkflow && workflow.Permissions == nil {
		pos := workflow.Pos
		if pos == nil {
			pos = &sourcetext.Position{Line: 1, Column: 1}
		}
		findings.Errorf(pos, "workflow-level \"permissions\" is required; declare the token permissions your jobs need, or use \"permissions: {}\" to grant none")
	}
	if !defaultLocations && !requireJob {
		return findings.Findings
	}
	if defaultLocations {
		independentlyTriggered := false
		for _, event := range workflow.On {
			if _, reusable := event.(*definition.WorkflowCallEvent); !reusable {
				independentlyTriggered = true
				break
			}
		}
		if !independentlyTriggered {
			return nil
		}
	}
	for _, job := range workflow.Jobs() {
		if job.Permissions == nil {
			findings.Errorf(job.Pos, "job %q does not declare token permissions; add \"permissions\" %s, or use \"permissions: {}\" to grant none", job.ID.Text(), permissionLocationDescription(defaultLocations))
		}
	}
	return findings.Findings
}

func permissionLocationDescription(allowWorkflow bool) string {
	if allowWorkflow {
		return "on this job or the workflow"
	}
	return "on this job"
}

func preparePermissionRequirements(c define.RuleInputs) (define.InputPreparation, error) {
	locations, err := c.Config.PermissionDeclarationLocations()
	if err != nil {
		return nil, err
	}
	c.RequiredPermissionLocations = locations
	return define.PreparedInputs{RuleInputs: c}, nil
}

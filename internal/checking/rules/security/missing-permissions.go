package security

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// MissingPermissionsRule is the single definition of the missing-permissions rule.
var MissingPermissionsRule = &define.Rule{
	Name:         "missing-permissions",
	Description:  "Report missing token permission declarations",
	Category:     "security",
	Subcategory:  "permissions",
	Recommended:  true,
	DefaultLevel: "error",
	Explanation:  "Report jobs with no permissions declared on the job or workflow. Set require-at to workflow or every-job to require permissions there. An array requires permissions at every listed location. When require-at is omitted, workflows triggered only by workflow_call are skipped. The workflow-call rule compares the caller's permissions with those requested by the called workflow.",
	Help:         "Add permissions on the workflow or on every job. If require-at is configured, add permissions at every location it lists. Use permissions: {} to grant none. See [CodeQL's explanation of missing workflow permissions](https://codeql.github.com/codeql-query-help/actions/actions-missing-workflow-permissions/). Set the rule to off to disable it.",
	Options:      func() any { return &configuration.MissingPermissionsOptions{} },
	Examples:     []rule.Example{{Description: "This job uses no token permissions.", Before: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", After: "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"}},
	Prepare:      preparePermissionRequirements,
	Checks:       func(c define.Inputs) define.Checks { return MissingPermissionsChecks(c.RequiredPermissionLocations) },
}

func MissingPermissionsChecks(locations []configuration.PermissionLocation) define.Checks {
	locations = slices.Clone(locations)
	return define.Checks{Workflow: func(workflow declaration.Workflow) []reporting.StepFinding {
		return MissingPermissions(workflow, locations)
	}}
}

func MissingPermissions(workflow declaration.Workflow, locations []configuration.PermissionLocation) []reporting.StepFinding {
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
			pos = &sourcepos.Position{Line: 1, Column: 1}
		}
		findings.Errorf(pos, "workflow-level \"permissions\" is required; declare the token permissions your jobs need, or use \"permissions: {}\" to grant none")
	}
	if !defaultLocations && !requireJob {
		return findings.Findings
	}
	if defaultLocations {
		independentlyTriggered := false
		for _, event := range workflow.On {
			if _, reusable := event.(*workflowlang.WorkflowCallEvent); !reusable {
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

func preparePermissionRequirements(c define.Inputs) (define.InputPreparation, error) {
	locations, err := c.Config.PermissionDeclarationLocations()
	if err != nil {
		return nil, err
	}
	c.RequiredPermissionLocations = locations
	return define.PreparedInputs{Inputs: c}, nil
}

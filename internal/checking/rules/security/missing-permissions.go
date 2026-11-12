package security

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func MissingPermissionsDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "missing-permissions", Description: "Report missing token permission declarations",
		Category: "security", Subcategory: "permissions", Maturity: "stable", Provider: "actionlint",
		Recommended: true, DefaultLevel: "error",
		Explanation:      "Report jobs with no permissions declared on the job or workflow. Set require-at to workflow or every-job to require permissions there. An array requires permissions at every listed location. When require-at is omitted, workflows triggered only by workflow_call are skipped. The workflow-call rule compares the caller's permissions with those requested by the called workflow.",
		Help:             "Add permissions on the workflow or on every job. If require-at is configured, add permissions at every location it lists. Use permissions: {} to grant none. See [CodeQL's explanation of missing workflow permissions](https://codeql.github.com/codeql-query-help/actions/actions-missing-workflow-permissions/). Set the rule to off to disable it.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-permissions",
		Options:          func() any { return &config.MissingPermissionsOptions{} },
		Examples:         []rule.Example{{Description: "This job uses no token permissions.", Before: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", After: "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"}},
	}
}

func MissingPermissionsCheck(locations []config.PermissionLocation) inputs.DeclarationCheck {
	locations = slices.Clone(locations)
	return inputs.DeclarationCheck{Declaration: MissingPermissionsDeclaration(), Workflow: func(workflow declaration.WorkflowView) []reporting.Finding {
		return MissingPermissions(workflow, locations)
	}}
}

func MissingPermissions(workflow declaration.WorkflowView, locations []config.PermissionLocation) []reporting.Finding {
	defaultLocations := len(locations) == 0
	requireWorkflow := slices.Contains(locations, config.WorkflowPermissions)
	requireJob := slices.Contains(locations, config.EveryJobPermissions)
	if defaultLocations && workflow.Permissions().Present() {
		return nil
	}
	findings := reporting.Collector{Rule: "missing-permissions"}
	if requireWorkflow && !workflow.Permissions().Present() {
		pos := reporting.ViewPosition(workflow.Pos())
		if pos == nil {
			pos = &workflowlang.Pos{Line: 1, Col: 1}
		}
		findings.Errorf(pos, "workflow-level \"permissions\" is required; declare the token permissions your jobs need, or use \"permissions: {}\" to grant none")
	}
	if !defaultLocations && !requireJob {
		return findings.Findings
	}
	if defaultLocations {
		independentlyTriggered := false
		for event := range workflow.On() {
			if _, reusable := event.WorkflowCallEvent(); !reusable {
				independentlyTriggered = true
				break
			}
		}
		if !independentlyTriggered {
			return nil
		}
	}
	for _, job := range workflow.Jobs() {
		if !job.Permissions().Present() {
			findings.Errorf(reporting.ViewPosition(job.Pos()), "job %q does not declare token permissions; add \"permissions\" %s, or use \"permissions: {}\" to grant none", job.ID().Value(), permissionLocationDescription(defaultLocations))
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

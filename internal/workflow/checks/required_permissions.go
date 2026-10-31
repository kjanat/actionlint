package checks

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// RequiredWorkflowPermissions checks the workflow declaration when workflow scope is enabled.
func RequiredWorkflowPermissions(n declaration.WorkflowView, scope string) []Finding {
	rule := findingCollector{name: "require-permissions"}
	if scope == "workflow" && !n.Permissions().Present() {
		pos := viewPosition(n.Pos())
		if pos == nil {
			pos = &workflowlang.Pos{Line: 1, Col: 1}
		}
		rule.Errorf(pos, "workflow-level \"permissions\" is required by the \"require-permissions\" policy. set \"permissions: {}\" and grant the scopes your jobs need")
	}
	return rule.out
}

// RequiredJobPermissions checks each job declaration when job scope is enabled.
func RequiredJobPermissions(n declaration.JobView, scope string) []Finding {
	rule := findingCollector{name: "require-permissions"}
	if scope == "job" && !n.Permissions().Present() {
		rule.Errorf(viewPosition(n.Pos()), "\"permissions\" is required in job %q by the \"require-permissions\" policy with \"scope: job\". set \"permissions: {}\" or grant the scopes this job needs", n.ID().Value())
	}
	return rule.out
}

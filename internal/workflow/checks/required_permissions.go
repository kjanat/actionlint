package checks

import (
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RequiredWorkflowPermissions checks the workflow declaration when workflow scope is enabled.
func RequiredWorkflowPermissions(n *workflowlang.Workflow, scope string) []Finding {
	rule := findingCollector{name: "require-permissions"}
	if scope == "workflow" && n.Permissions == nil {
		pos := n.Pos
		if pos == nil {
			pos = &workflowlang.Pos{Line: 1, Col: 1}
		}
		rule.Errorf(pos, "workflow-level \"permissions\" is required by the \"require-permissions\" policy. set \"permissions: {}\" and grant the scopes your jobs need")
	}
	return rule.out
}

// RequiredJobPermissions checks each job declaration when job scope is enabled.
func RequiredJobPermissions(n *workflowlang.Job, scope string) []Finding {
	rule := findingCollector{name: "require-permissions"}
	if scope == "job" && n.Permissions == nil {
		rule.Errorf(n.Pos, "\"permissions\" is required in job %q by the \"require-permissions\" policy with \"scope: job\". set \"permissions: {}\" or grant the scopes this job needs", n.ID.Value)
	}
	return rule.out
}

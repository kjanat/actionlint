package checks

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// MissingWorkflowPermissions reports jobs whose token permissions fall back to
// repository defaults. Workflows callable only through workflow_call inherit
// the caller's token ceiling; check the caller rather than requiring a duplicate
// declaration in the callee.
func MissingWorkflowPermissions(w declaration.WorkflowView) []Finding {
	if w.Permissions().Present() {
		return nil
	}
	for event := range w.On() {
		if _, reusable := event.WorkflowCallEvent(); reusable {
			continue
		}
		var out []Finding
		for _, job := range w.Jobs() {
			if !job.Permissions().Present() {
				out = append(out, warning("missing-workflow-permissions", "", viewPosition(job.Pos()),
					"job %q has no explicit token permissions and inherits repository defaults; declare least-privilege \"permissions\" at workflow or job scope", job.ID().Value()))
			}
		}
		return out
	}
	return nil
}

// RequiredWorkflowPermissions checks the workflow declaration when workflow scope is enabled.
func RequiredWorkflowPermissions(n declaration.WorkflowView, scope string) []Finding {
	rule := findingCollector{name: "explicit-permissions"}
	if scope == "workflow" && !n.Permissions().Present() {
		pos := viewPosition(n.Pos())
		if pos == nil {
			pos = &workflowlang.Pos{Line: 1, Col: 1}
		}
		rule.Errorf(pos, "workflow-level \"permissions\" is required by the \"explicit-permissions\" policy. set \"permissions: {}\" and grant the scopes your jobs need")
	}
	return rule.out
}

// RequiredJobPermissions checks each job declaration when job scope is enabled.
func RequiredJobPermissions(n declaration.JobView, scope string) []Finding {
	rule := findingCollector{name: "explicit-permissions"}
	if scope == "job" && !n.Permissions().Present() {
		rule.Errorf(viewPosition(n.Pos()), "\"permissions\" is required in job %q by the \"explicit-permissions\" policy with \"scope: job\". set \"permissions: {}\" or grant the scopes this job needs", n.ID().Value())
	}
	return rule.out
}

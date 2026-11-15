package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// PermissionsRule is the single definition of the permissions rule.
var PermissionsRule = &define.Rule{Name: "permissions", Description: "Validate token permission scopes and access levels", Category: "correctness", Subcategory: "permissions", Recommended: true, Help: "Use supported permission scopes and read, write or none access levels.", Examples: []rule.Example{rule.WorkflowExample("Choose an allowed access level for the permission scope.",
	"    permissions:\n      contents: execute\n", "    permissions:\n      contents: read\n")},
	Checks: define.Fixed(define.Checks{Workflow: checkWorkflowPermissions, Job: checkJobPermissions}),
}

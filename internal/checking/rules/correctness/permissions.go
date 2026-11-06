package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// PermissionsDeclaration describes the permissions rule.
func PermissionsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "permissions", Description: "Validate token permission scopes and access levels", Category: "correctness", Subcategory: "permissions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate token permission scopes and access levels.", Help: "Use supported permission scopes and read, write or none access levels.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#permissions", Examples: []rule.Example{rule.WorkflowExample("Choose an allowed access level for the permission scope.",
		"    permissions:\n      contents: execute\n", "    permissions:\n      contents: read\n")}}
}

// PermissionsCheck binds the rule to workflow and job declarations.
func PermissionsCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: PermissionsDeclaration(), Workflow: checkWorkflowPermissions, Job: checkJobPermissions}
}

package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// MissingWorkflowPermissionsDeclaration describes the missing-workflow-permissions rule.
func MissingWorkflowPermissionsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "missing-workflow-permissions", Description: "Report jobs that inherit repository token permissions", Category: "security", Subcategory: "permissions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Report each job without explicit workflow-level or job-level token permissions when the workflow has a trigger other than workflow_call. Mirrors CodeQL's actions/missing-workflow-permissions check. Workflows triggered only by workflow_call inherit the caller's token ceiling; callers must declare permissions. This check does not infer the token scopes required by arbitrary actions. Warning when enabled; error with --strict.", Help: "Declare least-privilege permissions at workflow or job scope. An empty mapping, read-all, write-all or a scope mapping counts as an explicit declaration; this check does not certify that the declared permissions are minimal. See [CodeQL's query](https://github.com/github/codeql/blob/main/actions/ql/src/Security/CWE-275/MissingActionsPermissions.ql).", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-workflow-permissions", Examples: []rule.Example{rule.WorkflowExample("Declare only the token permissions this job needs; this job uses no token scopes.",
		"", "    permissions: {}\n")}, DefaultLevel: "warn"}
}

// MissingWorkflowPermissionsCheck binds the rule to workflow declarations.
func MissingWorkflowPermissionsCheck() inputs.DeclarationCheck {
	return inputs.DeclarationCheck{Declaration: MissingWorkflowPermissionsDeclaration(), Workflow: MissingWorkflowPermissions}
}

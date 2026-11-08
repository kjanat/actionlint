package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// WorkflowCallDeclaration describes the workflow-call rule.
func WorkflowCallDeclaration() rule.Declaration {
	return rule.Declaration{Name: "workflow-call", Description: "Validate reusable workflow references and their declared inputs and outputs", Category: "correctness", Subcategory: "workflows", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Check reusable workflow references and report missing or undeclared inputs and secrets. For local calls with available metadata, check the caller's permission and cache limits against the called workflow. The expression-reference rule checks references to workflow outputs.", Help: "Correct the reference and supply required inputs and secrets. For permission or cache conflicts, adjust the caller's grant or reduce the called workflow's requirements.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#workflow-call"}
}

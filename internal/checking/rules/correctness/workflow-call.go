package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// WorkflowCallDeclaration describes the workflow-call rule.
func WorkflowCallDeclaration() rule.Declaration {
	return rule.Declaration{Name: "workflow-call", Description: "Validate reusable workflow references and their declared inputs and outputs", Category: "correctness", Subcategory: "workflows", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate reusable workflow references and their declared inputs and outputs.", Help: "Match the reusable workflow reference and its declared input/output contract.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#workflow-call"}
}

package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// WorkflowRunNamesDeclaration describes the workflow-run-names rule.
func WorkflowRunNamesDeclaration() rule.Declaration {
	return rule.Declaration{Name: "workflow-run-names", Description: "Report workflow_run references to unknown repository workflows", Category: "correctness", Subcategory: "workflows", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report workflow_run references to unknown repository workflows.", Help: "Use the exact name of an existing workflow in workflows; names are matched independently of filenames.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#workflow-run-names"}
}

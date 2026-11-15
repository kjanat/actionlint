package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// WorkflowRunNamesRule is the single definition of the workflow-run-names rule.
var WorkflowRunNamesRule = &define.Rule{Name: "workflow-run-names", Description: "Report workflow_run references to unknown repository workflows", Category: "correctness", Subcategory: "workflows", Recommended: true, Help: "Use the exact name of an existing workflow in workflows; names are matched independently of filenames.",
	Prepare: prepareWorkflowRunNames,
	Checks:  func(c define.Inputs) define.Checks { return WorkflowRunNamesChecks(c.WorkflowNameInventory) },
	Enabled: workflowRunNamesAvailable,
}

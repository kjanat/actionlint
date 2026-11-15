package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

// WorkflowCallRule is the single definition of the workflow-call rule.
var WorkflowCallRule = &define.Rule{
	Name:        "workflow-call",
	Description: "Validate reusable workflow references and their declared inputs and outputs",
	Category:    "correctness",
	Subcategory: "workflows",
	Recommended: true,
	Explanation: "Check reusable workflow references and report missing or undeclared inputs and secrets using available local or remote metadata. Compare the caller's known token permissions with the called workflow's declared job permissions. Cache limits are checked for local calls. The expression-reference rule checks references to workflow outputs.",
	Help:        "Correct the reference and supply required inputs and secrets. For permission or cache conflicts, adjust the caller's grant or reduce the called workflow's requirements.",
	Prepare:     define.PrepareWorkflowMetadata,
	Checks: func(c define.Inputs) define.Checks {
		workflow, _ := c.SourceSnapshot.WorkflowView()
		logger := reporting.Logger{Tag: "workflow-call", Writer: c.Debug}
		return WorkflowCallChecks(workflow, c.Workflows, configuration.RepositoryFacts(c.Config, c.Repository), logger.Debug)
	},
}

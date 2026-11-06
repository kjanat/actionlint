package correctness

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	namecheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-run-names"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

// WorkflowRunNamesCheck checks trigger filters against the supplied inventory.
// It does not read files or depend on prior callbacks.
func WorkflowRunNamesCheck(inventory namemodel.Inventory) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: WorkflowRunNamesDeclaration(),
		Workflow: func(workflow workflowdecl.WorkflowView) []reporting.Finding {
			return reporting.FromDiagnostics(namecheck.Check(inventory, namecheck.WorkflowRunFilters(workflow)))
		},
	}
}

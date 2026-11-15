package correctness

import (
	"fmt"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
	"actionlint.kjanat.dev/v2/internal/results"
)

// WorkflowRunNamesCheck checks trigger filters against the supplied inventory.
// It does not read files or depend on prior callbacks.
func WorkflowRunNamesCheck(inventory namemodel.Inventory) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{
		Declaration: WorkflowRunNamesDeclaration(),
		Workflow: func(workflow workflowdecl.Workflow) []reporting.Finding {
			return reporting.FromDiagnostics(workflowRunNameDiagnostics(inventory, WorkflowRunFilters(workflow)))
		},
	}
}

// WorkflowRunFilters returns declared producer-name filters without loading an inventory.
func WorkflowRunFilters(w workflowdecl.Workflow) []*workflowlang.String {
	if !w.Present() {
		return nil
	}
	var references []*workflowlang.String
	for _, event := range w.On {
		if hook, ok := event.(*workflowlang.WebhookEvent); ok && hook != nil && hook.Hook.Text() == "workflow_run" {
			references = append(references, hook.Workflows...)
		}
	}
	return references
}

// workflowRunNameDiagnostics verifies name filters against a complete repository inventory.
// Unknown expressions and incomplete inventories cannot establish missing producers.
func workflowRunNameDiagnostics(n namemodel.Inventory, references []*workflowlang.String) []results.Diagnostic {
	var findings []results.Diagnostic
	if len(references) == 0 {
		return findings
	}
	report := func(pos *workflowlang.Pos, format string, args ...any) {
		findings = append(findings, results.Diagnostic{Rule: "workflow-run-names", Message: fmt.Sprintf(format, args...), Start: sourcepos.Position{Line: pos.Line, Column: pos.Col}})
	}
	if !n.Complete() {
		return findings
	}
	var patterns []string
	allKnown, missing := true, false
	for _, ref := range references {
		if ref == nil {
			allKnown = false
			continue
		}
		value := ref.Value
		if literal := exprsyntax.LiteralExpressionValue(value); literal != nil {
			value = *literal
		} else if ref.ContainsExpression() {
			allKnown = false
			continue
		}
		patterns = append(patterns, value)
		if n.Missing(value) {
			missing = true
			report(ref.Pos, "workflow_run references workflow pattern %q, but no workflow name matches it in this repository", value)
		}
	}
	if allKnown && !missing && n.ExcludesAll(patterns) {
		report(references[0].Pos, "workflow_run workflow-name filters exclude every matching workflow in this repository")
	}
	return findings
}

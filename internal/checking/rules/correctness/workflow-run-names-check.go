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
		Workflow: func(workflow workflowdecl.WorkflowView) []reporting.Finding {
			return reporting.FromDiagnostics(workflowRunNameDiagnostics(inventory, WorkflowRunFilters(workflow)))
		},
	}
}

// WorkflowRunFilters returns declared producer-name filters without loading an inventory.
func WorkflowRunFilters(w workflowdecl.WorkflowView) []workflowdecl.StringView {
	var references []workflowdecl.StringView
	for event := range w.On() {
		if hook, ok := event.WebhookEvent(); ok && hook.Hook().Value() == "workflow_run" {
			for reference := range hook.Workflows() {
				references = append(references, reference)
			}
		}
	}

	return references
}

// workflowRunNameDiagnostics verifies name filters against a complete repository inventory.
// Unknown expressions and incomplete inventories cannot establish missing producers.
func workflowRunNameDiagnostics(n namemodel.Inventory, references []workflowdecl.StringView) []results.Diagnostic {
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
		if !ref.Present() {
			allKnown = false
			continue
		}
		value := ref.Value()
		if literal := exprsyntax.LiteralExpressionValue(value); literal != nil {
			value = *literal
		} else if ref.ContainsExpression() {
			allKnown = false
			continue
		}
		patterns = append(patterns, value)
		if n.Missing(value) {
			missing = true
			report(workflowRunNamePosition(ref), "workflow_run references workflow pattern %q, but no workflow name matches it in this repository", value)
		}
	}
	if allKnown && !missing && n.ExcludesAll(patterns) {
		report(workflowRunNamePosition(references[0]), "workflow_run workflow-name filters exclude every matching workflow in this repository")
	}
	return findings
}

func workflowRunNamePosition(value workflowdecl.StringView) *workflowlang.Pos {
	position, present := value.Pos()
	if !present {
		return nil
	}
	return &position
}

// Package workflownames checks workflow_run filters against known workflow names.
package workflownames

import (
	"fmt"

	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

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

// Check verifies name filters against a complete repository inventory.
// Unknown expressions and incomplete inventories cannot establish missing producers.
func Check(n namemodel.Inventory, references []*workflowlang.String) []results.Diagnostic {
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

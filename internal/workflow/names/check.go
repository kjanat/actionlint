package names

import (
	"fmt"

	"actionlint.kjanat.dev/diagnostic"
	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// WorkflowRunFilters returns declared producer-name filters without loading an inventory.
func WorkflowRunFilters(w *workflowlang.Workflow) []*workflowlang.String {
	var references []*workflowlang.String
	for _, event := range w.On {
		if hook, ok := event.(*workflowlang.WebhookEvent); ok && hook.Hook.Value == "workflow_run" {
			references = append(references, hook.Workflows...)
		}
	}

	return references
}

// CheckWorkflowRun verifies name filters against a complete repository inventory.
// Unknown expressions and incomplete inventories cannot establish missing producers.
func (n Names) CheckWorkflowRun(references []*workflowlang.String) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	if len(references) == 0 {
		return findings
	}
	report := func(pos *workflowlang.Pos, format string, args ...any) {
		findings = append(findings, diagnostic.Diagnostic{Rule: "workflow-run-names", Message: fmt.Sprintf(format, args...), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}})
	}
	if !n.Complete {
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
		if literal := exprlang.LiteralExpressionValue(value); literal != nil {
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

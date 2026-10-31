package names

import (
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

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

// CheckWorkflowRun verifies name filters against a complete repository inventory.
// Unknown expressions and incomplete inventories cannot establish missing producers.
func (n Names) CheckWorkflowRun(references []workflowdecl.StringView) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	if len(references) == 0 {
		return findings
	}
	report := func(pos *workflowlang.Pos, format string, args ...any) {
		findings = append(findings, diagnostic.Diagnostic{Rule: "workflow-run-names", Message: fmt.Sprintf(format, args...), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}})
	}
	if !n.complete {
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
		if literal := exprlang.LiteralExpressionValue(value); literal != nil {
			value = *literal
		} else if ref.ContainsExpression() {
			allKnown = false
			continue
		}
		patterns = append(patterns, value)
		if n.Missing(value) {
			missing = true
			report(namePosition(ref), "workflow_run references workflow pattern %q, but no workflow name matches it in this repository", value)
		}
	}
	if allKnown && !missing && n.ExcludesAll(patterns) {
		report(namePosition(references[0]), "workflow_run workflow-name filters exclude every matching workflow in this repository")
	}
	return findings
}

func namePosition(value workflowdecl.StringView) *workflowlang.Pos {
	position, present := value.Pos()
	if !present {
		return nil
	}
	return &position
}

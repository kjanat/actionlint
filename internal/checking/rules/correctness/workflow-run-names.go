package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	namemodel "actionlint.kjanat.dev/v2/internal/model/workflows/names"
	"actionlint.kjanat.dev/v2/internal/results"
)

// WorkflowRunNamesRule is the single definition of the workflow-run-names rule.
var WorkflowRunNamesRule = &define.Rule{
	Name:        "workflow-run-names",
	Description: "Report workflow_run references to unknown repository workflows",
	Category:    "correctness",
	Subcategory: "workflows",
	Recommended: true,
	Help:        "Use the exact name of an existing workflow in workflows; names are matched independently of filenames.",
	Prepare:     prepareWorkflowRunNames,
	Checks:      func(c define.Inputs) define.Checks { return WorkflowRunNamesChecks(c.WorkflowNameInventory) },
	Enabled:     define.Inputs.InProjectWorkflows,
}

// WorkflowRunNamesChecks checks trigger filters against the supplied inventory.
// It does not read files or depend on prior callbacks.
func WorkflowRunNamesChecks(inventory namemodel.Inventory) define.Checks {
	return define.Checks{
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
		findings = append(findings, results.Diagnostic{Message: fmt.Sprintf(format, args...), Start: sourcepos.Position{Line: pos.Line, Column: pos.Col}})
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

func prepareWorkflowRunNames(c define.Inputs) (define.InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, "workflow-run-names") == "off" {
		return define.PreparedInputs{Inputs: c}, nil
	}
	workflow, available := c.SourceSnapshot.WorkflowView()
	if !available || len(WorkflowRunFilters(workflow)) == 0 {
		return define.PreparedInputs{Inputs: c}, nil
	}
	inventory, err := c.ProjectWorkflowNames()
	if err != nil {
		return nil, err
	}
	if !inventory.Complete() {
		return define.UnavailableInputs{Reason: "not every repository workflow name could be determined"}, nil
	}
	c.WorkflowNameInventory = inventory
	return define.PreparedInputs{Inputs: c}, nil
}

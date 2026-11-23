package correctness

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
	"actionlint.kjanat.dev/v2/internal/results"
)

var WorkflowRunNamesRule = &define.Rule{
	Name:        "workflow-run-names",
	Description: "Report workflow_run references to unknown repository workflows",
	Category:    "correctness",
	Subcategory: "workflows",
	Recommended: true,
	Help:        "Use the name of an existing workflow, or a pattern that matches one. Filenames do not match.",
	Prepare:     prepareWorkflowRunNames,
	Checks:      func(c define.RuleInputs) define.Checks { return WorkflowRunNamesChecks(c.WorkflowNameInventory) },
	Enabled:     define.RuleInputs.InProjectWorkflows,
}

func WorkflowRunNamesChecks(inventory names.Inventory) define.Checks {
	return define.Checks{
		Workflow: func(workflow workflows.BoundWorkflow) []reporting.StepFinding {
			return reporting.FromDiagnostics(workflowRunNameDiagnostics(inventory, WorkflowRunFilters(workflow)))
		},
	}
}

func WorkflowRunFilters(w workflows.BoundWorkflow) []*definition.String {
	if !w.Present() {
		return nil
	}
	var references []*definition.String
	for _, event := range w.On {
		if hook, ok := event.(*definition.WebhookEvent); ok && hook != nil && hook.Hook.Text() == "workflow_run" {
			references = append(references, hook.Workflows...)
		}
	}
	return references
}

// Unknown expressions and incomplete inventories cannot establish missing producers.
func workflowRunNameDiagnostics(n names.Inventory, references []*definition.String) []results.Diagnostic {
	var findings []results.Diagnostic
	if len(references) == 0 {
		return findings
	}
	report := func(pos *sourcetext.Position, format string, args ...any) {
		findings = append(findings, results.Diagnostic{Message: fmt.Sprintf(format, args...), Start: *pos})
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
		if literal := syntax.LiteralExpressionValue(value); literal != nil {
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

func prepareWorkflowRunNames(c define.RuleInputs) (define.InputPreparation, error) {
	if c.Rules.DiagnosticLevel(c.Config, "workflow-run-names") == "off" {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	workflow, available := c.SourceSnapshot.WorkflowView()
	if !available || len(WorkflowRunFilters(workflow)) == 0 {
		return define.PreparedInputs{RuleInputs: c}, nil
	}
	inventory, err := c.ProjectWorkflowNames()
	if err != nil {
		return nil, err
	}
	if !inventory.Complete() {
		return define.UnavailableInputs{Reason: "not every repository workflow name could be determined"}, nil
	}
	c.WorkflowNameInventory = inventory
	return define.PreparedInputs{RuleInputs: c}, nil
}

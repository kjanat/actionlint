package documentparse

import (
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

// Parse adapts syntax findings to the analysis host's diagnostic state.
func Parse(source []byte) (*workflowlang.Workflow, []*findingmodel.Finding) {
	workflow, findings := workflowparser.Parse(source)
	return workflow, adaptSyntaxFindings(findings)
}

func adaptSyntaxFindings(findings []*workflowparser.SyntaxError) []*findingmodel.Finding {
	errors := make([]*findingmodel.Finding, 0, len(findings))
	for _, finding := range findings {
		errors = append(errors, &findingmodel.Finding{Message: finding.Message, Start: diagmodel.Position{Line: finding.Line, Column: finding.Column}, Rule: finding.Kind})
	}
	return errors
}

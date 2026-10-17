package engine

import (
	diagmodel "actionlint.kjanat.dev/diagnostic"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Parse adapts syntax findings to the analysis host's diagnostic state.
func Parse(source []byte) (*workflowlang.Workflow, []*findingmodel.Finding) {
	workflow, findings := workflowlang.Parse(source)
	return workflow, adaptSyntaxFindings(findings)
}

func adaptSyntaxFindings(findings []*workflowlang.SyntaxError) []*findingmodel.Finding {
	errors := make([]*findingmodel.Finding, 0, len(findings))
	for _, finding := range findings {
		errors = append(errors, &findingmodel.Finding{Message: finding.Message, Start: diagmodel.Position{Line: finding.Line, Column: finding.Column}, Rule: finding.Kind})
	}
	return errors
}

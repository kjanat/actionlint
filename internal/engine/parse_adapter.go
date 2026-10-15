package engine

import (
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Parse adapts syntax findings to the analysis host's diagnostic state.
func Parse(source []byte) (*workflowlang.Workflow, []*Error) {
	workflow, findings := workflowlang.Parse(source)
	return workflow, adaptSyntaxFindings(findings)
}

func adaptSyntaxFindings(findings []*workflowlang.SyntaxError) []*Error {
	errors := make([]*Error, 0, len(findings))
	for _, finding := range findings {
		errors = append(errors, &Error{Message: finding.Message, Line: finding.Line, Column: finding.Column, Kind: finding.Kind})
	}
	return errors
}

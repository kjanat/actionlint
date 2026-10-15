package engine

// ParseDocument adapts parser errors to the shared diagnostic contract.
func ParseDocument(source []byte) (*Workflow, []Diagnostic) {
	workflow, errors := Parse(source)
	diagnostics := make([]Diagnostic, 0, len(errors))
	for _, err := range errors {
		diagnostics = append(diagnostics, err.diagnostic(source))
	}
	return workflow, diagnostics
}

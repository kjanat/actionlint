package workflow

import (
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
)

// SyntaxError describes a syntax failure independently of lint rule state.
type SyntaxError struct {
	Message string
	Line    int
	Column  int
	Kind    string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s [%s]", e.Line, e.Column, e.Message, e.Kind)
}

// Diagnostic projects a syntax failure into the shared reporting contract.
func (e *SyntaxError) Diagnostic(source []byte) diagnostic.Diagnostic {
	snippet, _ := sourcemap.SourceLine(string(source), e.Line)
	return diagnostic.Diagnostic{
		Rule: e.Kind, Message: e.Message,
		Start:   diagnostic.Position{Line: e.Line, Column: e.Column},
		End:     diagnostic.Position{Line: e.Line, Column: text.TokenEndColumn(snippet, e.Column)},
		Snippet: snippet,
	}
}

// ParseDocument returns syntax and diagnostics without constructing an analysis host.
func ParseDocument(source []byte) (*Workflow, []diagnostic.Diagnostic) {
	tree, findings := Parse(source)
	diagnostics := make([]diagnostic.Diagnostic, 0, len(findings))
	for _, finding := range findings {
		diagnostics = append(diagnostics, finding.Diagnostic(source))
	}
	return tree, diagnostics
}

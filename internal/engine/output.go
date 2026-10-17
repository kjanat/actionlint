package engine

import (
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/yamlsource"
)

type DiagnosticPosition = diagnostic.Position
type Diagnostic = diagnostic.Diagnostic
type DiagnosticFix = diagnostic.Fix
type DiagnosticEdit = diagnostic.Edit

func (e *Error) diagnostic(source []byte) Diagnostic {
	source = e.sourceFor(source)
	end := e.End
	if end == (DiagnosticPosition{}) {
		end = DiagnosticPosition{Line: e.Start.Line, Column: e.Start.Column + 1}
	}
	snippet, _ := yamlsource.DiagnosticLine(source, e.Start.Line, e.rawScriptSource)
	if e.End == (DiagnosticPosition{}) && snippet != "" {
		end.Column = yamlsource.InclusiveEnd(snippet, e.Start.Column, e.endColumn) + 1
	}
	if e.endPosition != nil {
		end = DiagnosticPosition{Line: e.endPosition.Line, Column: e.endPosition.Col}
	}
	var fixes []DiagnosticFix
	if len(e.Fixes) > 0 {
		fixes = make([]DiagnosticFix, len(e.Fixes))
	}
	for i, fix := range e.Fixes {
		fixes[i] = fix
		fixes[i].Edits = make([]DiagnosticEdit, len(fix.Edits))
		for j, edit := range fix.Edits {
			if edit.Path == "" {
				edit.Path = e.Path
			}
			fixes[i].Edits[j] = edit
		}
	}
	d := e.Diagnostic
	d.End, d.Fixes = end, fixes
	if snippet != "" || d.Snippet == "" {
		d.Snippet = snippet
	}
	return d
}

// findingOf attaches analysis provenance without translating the diagnostic.
func findingOf(d Diagnostic) *Error { return &Error{Diagnostic: d} }

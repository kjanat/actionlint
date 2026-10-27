package finding

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
)

// Canonical resolves source extents and fix paths into the public diagnostic contract.
func (e *Finding) Canonical(source []byte) diagnostic.Diagnostic {
	source = e.SourceFor(source)
	end := e.End
	if end == (diagnostic.Position{}) {
		end = diagnostic.Position{Line: e.Start.Line, Column: e.Start.Column + 1}
		if e.UnavailableSource.Present() {
			end = e.Start
		}
	}
	snippet, _ := sourcemap.DiagnosticLine(source, e.Start.Line, e.RawScriptSource)
	if !e.UnavailableSource.Present() && e.End == (diagnostic.Position{}) && snippet != "" {
		end.Column = text.InclusiveEnd(snippet, e.Start.Column, e.EndColumn) + 1
	}
	if e.EndPosition != nil {
		end = diagnostic.Position{Line: e.EndPosition.Line, Column: e.EndPosition.Col}
	}
	var fixes []diagnostic.Fix
	if len(e.Fixes) > 0 {
		fixes = make([]diagnostic.Fix, len(e.Fixes))
	}
	for i, fix := range e.Fixes {
		fixes[i] = fix
		fixes[i].Edits = make([]diagnostic.Edit, len(fix.Edits))
		for j, edit := range fix.Edits {
			if edit.Path == "" {
				edit.Path = e.Path
			}
			fixes[i].Edits[j] = edit
		}
	}
	d := e.Diagnostic
	d.End, d.Fixes = end, fixes
	if e.UnavailableSource.Present() {
		d.Reference, d.Fixes = nil, nil
	}
	if snippet != "" || d.Snippet == "" {
		d.Snippet = snippet
	}
	return d
}

// FromDiagnostic attaches analysis provenance without translating the diagnostic.
func FromDiagnostic(d diagnostic.Diagnostic) *Finding { return &Finding{Diagnostic: d} }

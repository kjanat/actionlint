package finding

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Canonical resolves source extents and fix paths into the public diagnostic
// contract. The result shares no storage with the finding: its Reference,
// Related and Fixes, including each fix's Revisions and Edits, are copies.
func (e *Finding) Canonical(source []byte) results.Diagnostic {
	source = e.sourceFor(source)
	end := e.End
	if end == (sourcepos.Position{}) {
		end = sourcepos.Position{Line: e.Start.Line, Column: e.Start.Column + 1}
		if e.UnavailableSource.Present() {
			end = e.Start
		}
	}
	snippet, _ := sourcemap.DiagnosticLine(source, e.Start.Line, e.RawScriptSource)
	if !e.UnavailableSource.Present() && e.End == (sourcepos.Position{}) && snippet != "" {
		end.Column = sourcepos.InclusiveEnd(snippet, e.Start.Column, e.EndColumn) + 1
	}
	if e.EndPosition != nil {
		end = sourcepos.Position{Line: e.EndPosition.Line, Column: e.EndPosition.Col}
	}
	d := e.Diagnostic.Clone()
	switch {
	case e.UnavailableSource.Present():
		d.Reference, d.Fixes = nil, nil
	case len(d.Fixes) == 0:
		d.Fixes = nil
	default:
		for i := range d.Fixes {
			if d.Fixes[i].Edits == nil {
				d.Fixes[i].Edits = []results.Edit{}
			}
			for j := range d.Fixes[i].Edits {
				if d.Fixes[i].Edits[j].Path == "" {
					d.Fixes[i].Edits[j].Path = e.Path
				}
			}
		}
	}
	if snippet == "" {
		snippet = e.Snippet
	}
	d.End, d.Snippet = end, snippet
	return d
}

// FromDiagnostic attaches analysis provenance without translating the results.
func FromDiagnostic(d results.Diagnostic) *Finding { return &Finding{Diagnostic: d} }

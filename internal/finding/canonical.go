package finding

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/text"
	"actionlint.kjanat.dev/v2/internal/yaml/sourcemap"
)

// Canonical resolves source extents and fix paths into the public diagnostic
// contract. The result shares no storage with the finding: its Reference,
// Related and Fixes, including each fix's Revisions and Edits, are copies.
func (e *Finding) Canonical(source []byte) diagnostic.Diagnostic {
	source = e.sourceFor(source)
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
	reference, fixes := cloneReference(e.Reference), canonicalFixes(e.Fixes, e.Path)
	if e.UnavailableSource.Present() {
		reference, fixes = nil, nil
	}
	if snippet == "" {
		snippet = e.Snippet
	}
	d := e.Diagnostic
	d.Reference = reference
	d.Related = slices.Clone(e.Related)
	d.End, d.Snippet, d.Fixes = end, snippet, fixes
	return d
}

// canonicalFixes copies fixes, filling an empty edit Path with path, the
// finding's file. It returns nil for no fixes.
func canonicalFixes(fixes []diagnostic.Fix, path string) []diagnostic.Fix {
	if len(fixes) == 0 {
		return nil
	}
	out := make([]diagnostic.Fix, 0, len(fixes))
	for _, fix := range fixes {
		out = append(out, canonicalFix(fix, path))
	}
	return out
}

// canonicalFix copies fix with its own Revisions and Edits. Edits is never
// nil, so JSON output writes "edits": [] for a fix without edits.
func canonicalFix(fix diagnostic.Fix, path string) diagnostic.Fix {
	edits := make([]diagnostic.Edit, 0, len(fix.Edits))
	for _, edit := range fix.Edits {
		if edit.Path == "" {
			edit.Path = path
		}
		edits = append(edits, edit)
	}
	fix.Revisions = maps.Clone(fix.Revisions)
	fix.Edits = edits
	return fix
}

// FromDiagnostic attaches analysis provenance without translating the diagnostic.
func FromDiagnostic(d diagnostic.Diagnostic) *Finding { return &Finding{Diagnostic: d} }

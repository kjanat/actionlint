package finding

import (
	"actionlint.kjanat.dev/v2/internal/results"
)

// canonical resolves fix paths into the public diagnostic contract. The result
// shares no storage with the finding: its Reference, Related and Fixes,
// including each fix's Revisions and Edits, are copies. results.Publish
// resolves its End and Snippet from the source it is published with.
func (e *Finding) canonical() results.Diagnostic {
	d := e.Diagnostic.Clone()
	switch {
	case e.Source.Unavailable.Present():
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
	return d
}

// Publish publishes the canonical diagnostic with the text its positions refer
// to: the finding's own Source when it is Foreign to snapshot, otherwise
// snapshot. The published source carries no suppression target.
func (e *Finding) Publish(snapshot []byte) results.PublishedDiagnostic {
	source := e.Source
	source.Suppression = nil
	if !e.Foreign(snapshot) {
		source.Content = snapshot
	}
	return results.Publish(e.canonical(), source)
}

// FromDiagnostic attaches analysis provenance without translating the results.
func FromDiagnostic(d results.Diagnostic) *Finding { return &Finding{Diagnostic: d} }

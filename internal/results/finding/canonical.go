package finding

import (
	"actionlint.kjanat.dev/v2/internal/results"
)

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

// Publish uses the finding's own Source when the finding is Foreign to
// snapshot, and snapshot otherwise.
func (e *Finding) Publish(snapshot []byte) results.PublishedDiagnostic {
	source := e.Source
	source.Suppression = nil
	if !e.Foreign(snapshot) {
		source.Content = snapshot
	}
	return results.Publish(e.canonical(), source)
}

func FromDiagnostic(d results.Diagnostic) *Finding { return &Finding{Diagnostic: d} }

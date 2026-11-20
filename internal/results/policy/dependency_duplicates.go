package policy

import (
	"bytes"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/internal/results"
)

// identicalDependencyFinding requires captured bytes as well as equal findings.
// Matching messages or excerpts alone do not establish equal revisions.
func identicalDependencyFinding(a, b results.PublishedDiagnostic) bool {
	if a.Origin != results.DependencyOrigin || b.Origin != results.DependencyOrigin ||
		a.Source.Content == nil || b.Source.Content == nil || !bytes.Equal(a.Source.Content, b.Source.Content) ||
		a.Source.RawScript != b.Source.RawScript || a.Source.Unavailable != b.Source.Unavailable {
		return false
	}
	left, leftPin, leftOK := results.DependencyAddress(a.Path)
	right, rightPin, rightOK := results.DependencyAddress(b.Path)
	if !leftOK || !rightOK || left != right || leftPin == rightPin {
		return false
	}
	normalize := func(d results.Diagnostic) results.Diagnostic {
		d = d.Clone()
		original := d.Path
		d.Path, d.NonBlocking = left, false
		if d.Reference != nil {
			if d.Reference.Document == original {
				d.Reference.Document = left
				d.Reference.Node = ""
			}
		}
		d.Related = withoutDependencyLocations(d.Related)
		for i := range d.Fixes {
			fix := &d.Fixes[i]
			if revision, ok := fix.Revisions[original]; ok {
				delete(fix.Revisions, original)
				fix.Revisions[left] = revision
			}
			for j := range fix.Edits {
				if fix.Edits[j].Path == original {
					fix.Edits[j].Path = left
				}
			}
		}
		return d
	}
	return reflect.DeepEqual(normalize(a.Diagnostic), normalize(b.Diagnostic))
}

func withoutDependencyLocations(related []results.Related) []results.Related {
	filtered := slices.DeleteFunc(slices.Clone(related), func(r results.Related) bool {
		return r.Message == results.IdenticalDependencyLocation
	})
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

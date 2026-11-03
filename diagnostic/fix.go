package diagnostic

import (
	"maps"
	"slices"
)

// Clone copies a complete alternative, including all edits and source revisions.
func (f Fix) Clone() Fix {
	f.Revisions = maps.Clone(f.Revisions)
	f.Edits = slices.Clone(f.Edits)
	return f
}

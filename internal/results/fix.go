package results

import (
	"maps"
	"slices"
)

func (f Fix) Clone() Fix {
	f.Revisions = maps.Clone(f.Revisions)
	f.Edits = slices.Clone(f.Edits)
	return f
}

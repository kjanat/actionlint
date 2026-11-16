package facts

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

// Mapping contains normalized names and source provenance for a declaration.
// Complete is false when an insertion or key depends on runtime values.
type Mapping struct {
	Reference source.Reference
	Scope     string
	Complete  bool
	// Entries are the mapping's declarations in source traversal order.
	Entries []mapping.Declaration
}

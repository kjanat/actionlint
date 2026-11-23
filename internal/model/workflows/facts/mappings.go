package facts

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

// Mapping is incomplete when an insertion or key depends on runtime values.
type Mapping struct {
	Reference sourcetext.Reference
	Scope     string
	Complete  bool
	// Entries are the mapping's declarations in source traversal order.
	Entries []mappings.Declaration
}

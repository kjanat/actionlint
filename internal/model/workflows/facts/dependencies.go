package facts

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Dependency is a statically known action or reusable-workflow reference.
// Its range is the complete YAML scalar, including quotes when present.
type Dependency struct {
	Reference  source.Reference
	Uses       string
	Kind       string
	Start, End results.Position
}

package facts

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Dependency ranges cover the complete YAML scalar, including quotes when present.
type Dependency struct {
	Reference sourcetext.Reference `json:"reference"`
	Uses      string               `json:"uses"`
	Kind      string               `json:"kind"`
	Start     sourcetext.Position  `json:"start"`
	End       sourcetext.Position  `json:"end"`
}

// ResolvedDependency holds Commit and Tree as different object kinds.
type ResolvedDependency struct {
	Dependency               Dependency
	Ref                      string
	RepositoryID             repository.ID
	Commit, Tree, ObservedAt string
}

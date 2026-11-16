package facts

import (
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Dependency is a statically known action or reusable-workflow reference.
// Its range is the complete YAML scalar, including quotes when present.
// The JSON form is what document process tools receive.
type Dependency struct {
	Reference sourcetext.Reference `json:"reference"`
	Uses      string               `json:"uses"`
	Kind      string               `json:"kind"`
	Start     sourcetext.Position  `json:"start"`
	End       sourcetext.Position  `json:"end"`
}

// ResolvedDependency binds a successful lookup to its captured declaration.
// Commit and Tree identify different object kinds; a tree is not a commit pin.
type ResolvedDependency struct {
	Dependency               Dependency
	Ref                      string
	RepositoryID             repository.ID
	Commit, Tree, ObservedAt string
}

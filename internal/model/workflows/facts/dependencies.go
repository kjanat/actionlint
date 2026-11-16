package facts

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Dependency is a statically known action or reusable-workflow reference.
// Its range is the complete YAML scalar, including quotes when present.
type Dependency struct {
	Reference  source.Reference
	Uses       string
	Kind       string
	Start, End source.Position
}

// ResolvedDependency binds a successful lookup to its captured declaration.
// Commit and Tree identify different object kinds; a tree is not a commit pin.
type ResolvedDependency struct {
	Dependency               Dependency
	Ref                      string
	RepositoryID             repository.ID
	Commit, Tree, ObservedAt string
}

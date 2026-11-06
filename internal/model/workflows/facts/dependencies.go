package facts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

// Dependency is a statically known action or reusable-workflow reference.
// Its range is the complete YAML scalar, including quotes when present.
type Dependency struct {
	Reference  sourcepos.Reference
	Uses       string
	Kind       string
	Start, End sourcepos.Position
}

// ResolvedDependency binds a successful lookup to its captured declaration.
// Commit and Tree identify different object kinds; a tree is not a commit pin.
type ResolvedDependency struct {
	Dependency               Dependency
	Ref                      string
	RepositoryID             uint64
	Commit, Tree, ObservedAt string
}

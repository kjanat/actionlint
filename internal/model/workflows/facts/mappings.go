package facts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

// Mapping contains normalized names and source provenance for a declaration.
// Complete is false when an insertion or key depends on runtime values.
type Mapping struct {
	Reference sourcepos.Reference
	Scope     string
	Complete  bool
	Entries   []Entry
}

// Entry separates an evaluated name from source expression text. Evaluated
// entries came from object expressions and must not be interpreted again.
type Entry struct {
	Name      string
	State     KeyState
	Source    string
	Evaluated bool
	Start     sourcepos.Position
}

// KeyState distinguishes a runtime name from a statically invalid declaration.
type KeyState string

const (
	UnknownKey KeyState = "unknown"
	KnownKey   KeyState = "known"
	InvalidKey KeyState = "invalid"
)

package toolsettings

import (
	"encoding/json"
	"maps"
	"slices"
)

// Spec describes the configuration accepted by a tool provider.
type Spec struct {
	Schema     json.RawMessage
	Shorthands map[string]Shorthand
	ReadsFiles bool
}

func (s Spec) Clone() Spec {
	s.Schema = slices.Clone(s.Schema)
	shorthands := maps.Clone(s.Shorthands)
	for name, shorthand := range s.Shorthands {
		shorthand.Path = slices.Clone(shorthand.Path)
		shorthands[name] = shorthand
	}
	s.Shorthands = shorthands
	return s
}

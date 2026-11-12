package configschema

import (
	"encoding/json"
	"maps"
	"slices"
)

// Settings describes the configuration accepted by a tool provider.
type Settings struct {
	Schema     json.RawMessage
	Shorthands map[string]Shorthand
	ReadsFiles bool
}

func (s Settings) Clone() Settings {
	s.Schema = slices.Clone(s.Schema)
	shorthands := maps.Clone(s.Shorthands)
	for name, shorthand := range s.Shorthands {
		shorthand.Path = slices.Clone(shorthand.Path)
		shorthands[name] = shorthand
	}
	s.Shorthands = shorthands
	return s
}

package toolsettings

import (
	"encoding/json"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

type Provider struct {
	// Rule is the provider's reporting rule. tools.<Rule.Name> and the
	// lint.rules entry take their names from it.
	Rule          rule.Descriptor
	Settings      Spec
	ProcessSchema json.RawMessage
}

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

package toolsettings

import (
	"encoding/json"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// Provider is what the printed configuration schema needs from an installed
// tool provider. A tool manifest builds it, so the schema does not depend on
// the manifest model.
type Provider struct {
	// Rule is the rule the provider reports under; tools.<Rule.Name> and the
	// lint.rules entry are named after it.
	Rule rule.Descriptor
	// Settings holds the provider's config_schema, its shorthands and
	// whether tools.<name>.config may name a configuration file.
	Settings Spec
	// ProcessSchema is a process provider's process.config_schema. It is
	// empty when the provider declares none.
	ProcessSchema json.RawMessage
}

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

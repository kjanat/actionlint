// Package lintconfig owns rule-setting syntax, independent of the analyzer and CLI.
package lintconfig

import (
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v4"
)

// Level controls one diagnostic, independently of its category or maturity.
type Level string

// UnmarshalYAML normalizes false to off and null to the default level.
// The zero value represents an omitted/null level, never an empty string input.
func (l *Level) UnmarshalYAML(n *yaml.Node) error {
	switch n.ShortTag() {
	case "!!null":
		*l = "default"
		return nil
	case "!!bool":
		var enabled bool
		if err := n.Decode(&enabled); err != nil {
			return err
		}
		if !enabled {
			*l = "off"
			return nil
		}
	case "!!str":
		value := Level(n.Value)
		if value.Valid() {
			*l = value
			return nil
		}
	}
	return errors.New("yaml: rule level must be default, off, on, info, warn, or error; empty strings are not allowed")
}

// MarshalYAML uses the canonical named level.
func (l Level) MarshalYAML() (any, error) {
	if l == "" {
		return "default", nil
	}
	return string(l), nil
}

func (l Level) Valid() bool {
	return slices.Contains([]Level{"default", "off", "on", "info", "warn", "error"}, l)
}

// Preset selects a baseline. All intentionally excludes nursery rules.
type Preset string

// UnmarshalYAML validates preset names rather than silently ignoring typos.
func (p *Preset) UnmarshalYAML(n *yaml.Node) error {
	if n.ShortTag() != "!!str" || !slices.Contains([]string{"recommended", "all", "none"}, n.Value) {
		return errors.New("yaml: rule preset must be recommended, all, or none")
	}
	*p = Preset(n.Value)
	return nil
}

// Setting accepts a level shorthand or a level/options mapping. Options are
// decoded into the rule's registered type, never an unvalidated free-form map.
type Setting struct {
	Level       Level `yaml:"level"`
	Options     any   `yaml:"options,omitempty"`
	optionsNode *yaml.Node
}

// UnmarshalYAML validates the common shape; the catalog validates options.
func (s *Setting) UnmarshalYAML(n *yaml.Node) error {
	var next Setting
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&next.Level); err != nil {
			return err
		}
	case yaml.MappingNode:
		var wire struct {
			Level   yaml.Node `yaml:"level"`
			Options yaml.Node `yaml:"options"`
		}
		if err := n.Load(&wire, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
			return err
		}
		if wire.Level.Kind == 0 {
			return errors.New("yaml: rule setting requires level")
		}
		if err := wire.Level.Decode(&next.Level); err != nil {
			return err
		}
		if wire.Options.Kind != 0 {
			next.optionsNode = &wire.Options
		}
	default:
		return errors.New("yaml: rule setting must be a level or level/options mapping")
	}
	if next.Level != "" && !next.Level.Valid() {
		return errors.New("yaml: rule level must be default, off, on, info, warn, or error")
	}
	*s = next
	return nil
}

// Group contains a concern-wide baseline and individual exceptions.
type Group struct {
	Preset Preset             `yaml:"preset,omitempty"`
	Level  Level              `yaml:"level,omitempty"`
	Rules  map[string]Setting `yaml:",inline"`
}

// UnmarshalYAML accepts a group level or a preset/rule mapping.
func (g *Group) UnmarshalYAML(n *yaml.Node) error {
	var next Group
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&next.Level); err != nil {
			return err
		}
	case yaml.MappingNode:
		type plain Group
		if err := n.Load((*plain)(&next), yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
			return err
		}
	default:
		return errors.New("yaml: rule group must be a level or mapping")
	}
	if next.Level != "" && !next.Level.Valid() {
		return fmt.Errorf("yaml: invalid rule group level %q", next.Level)
	}
	*g = next
	return nil
}

// MarshalYAML preserves group shorthand across configuration round trips.
func (g Group) MarshalYAML() (any, error) {
	if g.Level != "" && g.Preset == "" && len(g.Rules) == 0 {
		return string(g.Level), nil
	}
	type plain Group
	return plain(g), nil
}

// DecodeOptions validates and decodes options using the catalog's type factory.
func (s *Setting) DecodeOptions(factory func() any) error {
	if s.optionsNode == nil {
		return nil
	}
	if factory == nil {
		return errors.New("rule does not accept options")
	}
	if s.optionsNode.Kind != yaml.MappingNode {
		return errors.New("options must be a mapping")
	}
	s.Options = factory()
	return s.optionsNode.Load(s.Options, yaml.WithV3Defaults(), yaml.WithKnownFields())
}

// Package rules owns rule-setting syntax, independent of the analyzer and CLI.
package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"github.com/kjanat/go-yaml/v4"
)

// Level controls one diagnostic, independently of its category or maturity.
type Level string

// UnmarshalYAML normalizes booleans to on/off and null to the default level.
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
		*l = "off"
		if enabled {
			*l = "on"
		}
		return nil
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

// UnmarshalYAML rejects unknown preset names.
func (p *Preset) UnmarshalYAML(n *yaml.Node) error {
	if n.ShortTag() != "!!str" || !slices.Contains([]string{"recommended", "all", "none"}, n.Value) {
		return errors.New("yaml: rule preset must be recommended, all, or none")
	}
	*p = Preset(n.Value)
	return nil
}

// Setting is one rule's level and options. In YAML it is a level shorthand or
// a level/options mapping. The options stay in their YAML form; LoadOptions
// decodes them into the rule's registered type on every call, so each caller
// gets its own copy.
type Setting struct {
	level Level
	// options is a mapping node, or nil when the setting has no options.
	// options: null also leaves it nil, which resets the rule's options.
	options *yaml.Node
}

// NewSetting returns a setting with the given level and options. An empty
// level selects the rule's default; nil options mean no options.
//
// options must form a mapping. A json.RawMessage is parsed as a JSON object,
// the form analyzer.Registration.Options uses. Any other value is encoded with
// the YAML encoder: struct fields are named by their yaml tags, json tags and
// MarshalJSON methods are ignored, and a value whose MarshalYAML returns a
// scalar, such as a disabled policy that encodes as false, is rejected.
func NewSetting(level Level, options any) (Setting, error) {
	setting := Setting{level: level}
	if raw, ok := options.(json.RawMessage); options == nil || ok && len(raw) == 0 {
		return setting, nil
	}
	node, err := encodeOptions(options)
	if err != nil {
		return Setting{}, fmt.Errorf("rule options: %w", err)
	}
	if node.ShortTag() == "!!null" {
		return setting, nil
	}
	if node.Kind != yaml.MappingNode {
		return Setting{}, fmt.Errorf("rule options must be a mapping, not %s", node.ShortTag())
	}
	owned, err := ownOptions(node)
	if err != nil {
		return Setting{}, fmt.Errorf("rule options: %w", err)
	}
	setting.options = owned
	return setting, nil
}

// Level returns the configured level. The empty level means the setting gave
// no level and the rule's default applies.
func (s Setting) Level() Level { return s.level }

// WithLevel returns a copy of the setting that uses level and keeps its options.
func (s Setting) WithLevel(level Level) Setting {
	s.level = level
	return s
}

// HasOptions reports whether the setting configures options.
func (s Setting) HasOptions() bool { return s.options != nil }

// LoadOptions decodes the options into dst, which must point to the rule's
// registered options type. It reports false and leaves dst unchanged when the
// setting has no options. dst is decoded from a copy of the stored node, so
// an UnmarshalYAML method in dst that keeps or edits its node cannot reach
// the setting.
func (s Setting) LoadOptions(dst any) (bool, error) {
	if s.options == nil {
		return false, nil
	}
	if err := cloneTree(s.options).Load(dst, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return false, err
	}
	return true, nil
}

// encodeOptions returns options as a YAML node. JSON is valid YAML flow
// syntax, so the YAML parser reads a json.RawMessage into the same tree the
// encoder builds for the equivalent Go value.
func encodeOptions(options any) (*yaml.Node, error) {
	raw, ok := options.(json.RawMessage)
	if !ok {
		var node yaml.Node
		if err := node.Encode(options); err != nil {
			return nil, err
		}
		return &node, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("json.RawMessage options are not valid JSON")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("json.RawMessage options must hold one JSON value")
	}
	return document.Content[0], nil
}

// ownOptions returns a copy of an options mapping with aliases and merge keys
// expanded, so the setting holds a tree that shares no node with the document
// it came from.
func ownOptions(n *yaml.Node) (*yaml.Node, error) {
	return configtree.Expand(n, map[*yaml.Node]bool{})
}

// cloneTree returns a deep copy of n through Content. n must be a tree without
// alias nodes, as ownOptions produces; Alias and Stream are not copied.
func cloneTree(n *yaml.Node) *yaml.Node {
	c := &yaml.Node{
		Kind: n.Kind, Style: n.Style, Tag: n.Tag, Value: n.Value, Anchor: n.Anchor,
		HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment,
		Line: n.Line, Column: n.Column,
	}
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = cloneTree(child)
		}
	}
	return c
}

// parseSetting reads a level shorthand or a level/options mapping. It checks
// the level and that options form a mapping; the catalog validates the
// options against the rule's type.
func parseSetting(n *yaml.Node) (Setting, error) {
	var next Setting
	switch n.Kind {
	case yaml.ScalarNode:
		if err := n.Decode(&next.level); err != nil {
			return Setting{}, err
		}
	case yaml.MappingNode:
		var wire struct {
			Level   yaml.Node `yaml:"level"`
			Options yaml.Node `yaml:"options"`
		}
		if err := n.Load(&wire, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
			return Setting{}, err
		}
		if wire.Level.Kind == 0 {
			return Setting{}, errors.New("yaml: rule setting requires level")
		}
		if err := wire.Level.Decode(&next.level); err != nil {
			return Setting{}, err
		}
		if wire.Options.Kind != 0 && wire.Options.ShortTag() != "!!null" {
			if wire.Options.Kind != yaml.MappingNode {
				return Setting{}, errors.New("yaml: options must be a mapping")
			}
			owned, err := ownOptions(&wire.Options)
			if err != nil {
				return Setting{}, err
			}
			next.options = owned
		}
	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode, yaml.StreamNode:
		// Only levels and mappings configure a setting.
		fallthrough
	default:
		return Setting{}, errors.New("yaml: rule setting must be a level or level/options mapping")
	}
	if next.level != "" && !next.level.Valid() {
		return Setting{}, errors.New("yaml: rule level must be default, off, on, info, warn, or error")
	}
	return next, nil
}

// settingYAML decodes one Setting inside a group mapping. Decoding through it
// lets go-yaml report a bad setting at the setting's own line and column.
type settingYAML struct{ setting Setting }

func (s *settingYAML) UnmarshalYAML(n *yaml.Node) error {
	setting, err := parseSetting(n)
	if err != nil {
		return err
	}
	s.setting = setting
	return nil
}

// MarshalYAML writes the level/options mapping. The options are decoded into
// a fresh generic value, so the encoder never sees the stored node.
func (s Setting) MarshalYAML() (any, error) {
	var wire struct {
		Level   Level `yaml:"level"`
		Options any   `yaml:"options,omitempty"`
	}
	wire.Level = s.level
	if _, err := s.LoadOptions(&wire.Options); err != nil {
		return nil, err
	}
	return wire, nil
}

// Group contains a concern-wide baseline and individual exceptions.
type Group struct {
	preset Preset
	level  Level
	rules  map[string]Setting
}

// groupWire is the YAML mapping MarshalYAML writes for a Group.
type groupWire struct {
	Preset Preset             `yaml:"preset,omitempty"`
	Level  Level              `yaml:"level,omitempty"`
	Rules  map[string]Setting `yaml:",inline"`
}

// NewGroup returns a group with a preset, a group level and per-rule
// settings. Empty preset and level leave the parent selection in place. The
// group keeps its own copy of rules.
func NewGroup(preset Preset, level Level, rules map[string]Setting) Group {
	return Group{preset: preset, level: level, rules: maps.Clone(rules)}
}

// Preset returns the group's baseline, or "" when the group sets none.
func (g Group) Preset() Preset { return g.preset }

// Level returns the level applied to every rule in the group, or "" when the
// group sets none.
func (g Group) Level() Level { return g.level }

// Rule returns the explicit setting for the named rule.
func (g Group) Rule(name string) (Setting, bool) {
	setting, ok := g.rules[name]
	return setting, ok
}

// IsZero reports whether the group sets no preset, level or rule. YAML
// encoding calls it to apply omitempty, because it cannot see private fields.
func (g Group) IsZero() bool { return g.preset == "" && g.level == "" && len(g.rules) == 0 }

// Rules yields each rule with an explicit setting, in no particular order.
func (g Group) Rules() iter.Seq2[string, Setting] { return maps.All(g.rules) }

// UnmarshalYAML accepts a group level or a preset/rule mapping.
func (g *Group) UnmarshalYAML(n *yaml.Node) error {
	var next struct {
		Preset Preset                 `yaml:"preset,omitempty"`
		Level  Level                  `yaml:"level,omitempty"`
		Rules  map[string]settingYAML `yaml:",inline"`
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.ShortTag() == "!!str" && n.Value == "all" {
			next.Preset = "all"
		} else if err := n.Decode(&next.Level); err != nil {
			return err
		}
	case yaml.MappingNode:
		if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
			return err
		}
	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode, yaml.StreamNode:
		// Only levels and mappings configure a group.
		fallthrough
	default:
		return errors.New("yaml: rule group must be a level or mapping")
	}
	if next.Level != "" && !next.Level.Valid() {
		return fmt.Errorf("yaml: invalid rule group level %q", next.Level)
	}
	var rules map[string]Setting
	if next.Rules != nil {
		rules = make(map[string]Setting, len(next.Rules))
		for name, decoded := range next.Rules {
			rules[name] = decoded.setting
		}
	}
	*g = Group{preset: next.Preset, level: next.Level, rules: rules}
	return nil
}

// MarshalYAML preserves group shorthand across configuration round trips.
func (g Group) MarshalYAML() (any, error) {
	if g.level != "" && g.preset == "" && len(g.rules) == 0 {
		return string(g.level), nil
	}
	return groupWire{Preset: g.preset, Level: g.level, Rules: maps.Clone(g.rules)}, nil
}

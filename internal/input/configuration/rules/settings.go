// Package rules owns rule-setting syntax, independent of the analyzer and CLI.
package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// Level controls one diagnostic, independently of its category or maturity.
type Level string

// Levels lists the named diagnostic levels in editor completion order.
// The returned slice belongs to the caller.
func Levels() []Level {
	return []Level{"default", "off", "on", "info", "warn", "error"}
}

// Categories uses the public rule contract's semantic category names.
// The returned slice belongs to the caller.
func Categories() []string { return rule.Categories() }

func choices[T ~string](values []T) string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// DecodeConfig normalizes booleans to on/off and null to the default level.
// The zero value represents an omitted/null level, never an empty string input.
func (l *Level) DecodeConfig(value any) error {
	switch selected := value.(type) {
	case nil:
		*l = "default"
		return nil
	case bool:
		*l = "off"
		if selected {
			*l = "on"
		}
		return nil
	case string:
		next := Level(selected)
		if next.Valid() {
			*l = next
			return nil
		}
	}
	return fmt.Errorf("rule level must be %s; empty strings are not allowed", choices(Levels()))
}

// EncodeConfig uses the canonical named level.
func (l Level) EncodeConfig() (any, error) {
	if l == "" {
		return "default", nil
	}
	return string(l), nil
}

func (l Level) Valid() bool {
	return slices.Contains(Levels(), l)
}

// Preset selects a baseline. All intentionally excludes nursery rules.
type Preset string

// Presets lists the accepted baseline selections in editor completion order.
// The returned slice belongs to the caller.
func Presets() []Preset { return []Preset{"recommended", "all", "none"} }

// Valid reports whether the preset is a named baseline selection.
func (p Preset) Valid() bool { return slices.Contains(Presets(), p) }

// DecodeConfig rejects unknown preset names.
func (p *Preset) DecodeConfig(value any) error {
	selected, ok := value.(string)
	if !ok || !Preset(selected).Valid() {
		return fmt.Errorf("rule preset must be %s", choices(Presets()))
	}
	*p = Preset(selected)
	return nil
}

// Setting is one rule's level and options. In configuration it is a level shorthand or
// a level/options mapping. The options stay in their configuration form; LoadOptions
// decodes them into the rule's registered type on every call, so each caller
// gets its own copy.
type Setting struct {
	level Level
	// options is an owned mapping, or nil when the setting has no options.
	// options: null also leaves it nil, which resets the rule's options.
	options map[string]any
}

// NewSetting returns a setting with the given level and options. An empty
// level selects the rule's default; nil options mean no options.
//
// options must form a mapping. A json.RawMessage is parsed as a JSON object,
// the form analyzer.Registration.Options uses. Any other value is encoded with
// the configuration encoder: struct fields are named by their yaml tags, json tags and
// MarshalJSON methods are ignored, and a value whose EncodeConfig returns a
// scalar, such as a disabled policy that encodes as false, is rejected.
func NewSetting(level Level, options any) (Setting, error) {
	setting := Setting{level: level}
	if raw, ok := options.(json.RawMessage); options == nil || ok && len(raw) == 0 {
		return setting, nil
	}
	value, err := encodeOptions(options)
	if err != nil {
		return Setting{}, fmt.Errorf("rule options: %w", err)
	}
	if value == nil {
		return setting, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		if _, boolean := value.(bool); boolean {
			return Setting{}, errors.New("rule options must be a mapping, got a boolean")
		}
		return Setting{}, errors.New("rule options must be a mapping")
	}
	setting.options = object
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
// setting has no options. dst is decoded from a copy of the stored values, so
// a DecodeConfig method in dst that keeps or edits its values cannot reach
// the setting.
func (s Setting) LoadOptions(dst any) (bool, error) {
	if s.options == nil {
		return false, nil
	}
	owned, err := binding.Encode(s.options)
	if err != nil {
		return false, err
	}
	if err := binding.Decode(owned, dst); err != nil {
		return false, err
	}
	return true, nil
}

// encodeOptions owns an options value, including already serialized JSON.
func encodeOptions(options any) (any, error) {
	if raw, ok := options.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, errors.New("json.RawMessage options are not valid JSON")
		}
		parsed, err := document.Parse("options.json", raw)
		if err != nil {
			return nil, err
		}
		return parsed.Value, nil
	}
	return binding.Encode(options)
}

// parseSetting reads a level shorthand or a level/options mapping. It checks
// the level and that options form a mapping; the catalog validates the
// options against the rule's type.
func parseSetting(value any) (Setting, error) {
	var next Setting
	if object, ok := value.(map[string]any); ok {
		for key := range object {
			if key != "level" && key != "options" {
				return Setting{}, binding.At(key, fmt.Errorf("unknown rule setting key %q", key))
			}
		}
		level, present := object["level"]
		if !present {
			return Setting{}, errors.New("rule setting requires level")
		}
		if err := next.level.DecodeConfig(level); err != nil {
			return Setting{}, binding.At("level", err)
		}
		if options := object["options"]; options != nil {
			mapping, ok := options.(map[string]any)
			if !ok {
				return Setting{}, binding.At("options", errors.New("options must be a mapping"))
			}
			owned, err := binding.Encode(mapping)
			if err != nil {
				return Setting{}, err
			}
			ownedMapping, ok := owned.(map[string]any)
			if !ok {
				return Setting{}, errors.New("encoded rule options must be a mapping")
			}
			next.options = ownedMapping
		}
	} else if err := next.level.DecodeConfig(value); err != nil {
		return Setting{}, err
	}
	return next, nil
}

// DecodeConfig validates a rule level or level/options mapping.
func (s *Setting) DecodeConfig(value any) error {
	next, err := parseSetting(value)
	if err != nil {
		return err
	}
	*s = next
	return nil
}

// EncodeConfig writes the level/options mapping. The options are decoded into
// a fresh value so callers cannot mutate the stored options.
func (s Setting) EncodeConfig() (any, error) {
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

// groupWire is the configuration mapping EncodeConfig writes for a Group.
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

// IsZero reports whether the group sets no preset, level or rule. The shared
// encoder uses it to apply omitempty to fields with private state.
func (g Group) IsZero() bool { return g.preset == "" && g.level == "" && len(g.rules) == 0 }

// Rules yields each rule with an explicit setting, in no particular order.
func (g Group) Rules() iter.Seq2[string, Setting] { return maps.All(g.rules) }

// DecodeConfig accepts a group level or a preset/rule mapping.
func (g *Group) DecodeConfig(value any) error {
	var next Group
	if object, ok := value.(map[string]any); ok {
		next.rules = make(map[string]Setting)
		for key, entry := range object {
			switch key {
			case "preset":
				if err := next.preset.DecodeConfig(entry); err != nil {
					return binding.At(key, err)
				}
			case "level":
				if err := next.level.DecodeConfig(entry); err != nil {
					return binding.At(key, err)
				}
			default:
				var setting Setting
				if err := setting.DecodeConfig(entry); err != nil {
					return binding.At(key, err)
				}
				next.rules[key] = setting
			}
		}
	} else if name, ok := value.(string); ok && name == "all" {
		next.preset = "all"
	} else if err := next.level.DecodeConfig(value); err != nil {
		return err
	}
	*g = next
	return nil
}

// EncodeConfig preserves group shorthand across configuration round trips.
func (g Group) EncodeConfig() (any, error) {
	if g.level != "" && g.preset == "" && len(g.rules) == 0 {
		return string(g.level), nil
	}
	return groupWire{Preset: g.preset, Level: g.level, Rules: maps.Clone(g.rules)}, nil
}

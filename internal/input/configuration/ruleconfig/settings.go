// Package ruleconfig decodes and encodes rule levels, presets, settings and groups.
package ruleconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configdecode"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"

	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/severity"
)

type Level string

// Levels lists the named diagnostic levels in editor completion order.
func Levels() []Level {
	return []Level{"default", "off", "on", "info", "warn", "error"}
}

func Categories() []string { return rule.Categories() }

func choices[T ~string](values []T) string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// DecodeConfig normalizes booleans to on/off and null to the default level.
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

func (l Level) EncodeConfig() (any, error) {
	if l == "" {
		return "default", nil
	}
	return string(l), nil
}

func (l Level) Valid() bool {
	return slices.Contains(Levels(), l)
}

func (l Level) Severity() (severity.Severity, bool) {
	switch l {
	case "info":
		return severity.Info, true
	case "warn":
		return severity.Warning, true
	case "error":
		return severity.Error, true
	}
	return "", false
}

type Preset string

// Presets lists the accepted baseline selections in editor completion order.
func Presets() []Preset { return []Preset{"recommended", "all", "none"} }

func (p Preset) Valid() bool { return slices.Contains(Presets(), p) }

func (p *Preset) DecodeConfig(value any) error {
	selected, ok := value.(string)
	if !ok || !Preset(selected).Valid() {
		return fmt.Errorf("rule preset must be %s", choices(Presets()))
	}
	*p = Preset(selected)
	return nil
}

type Setting struct {
	level Level
	// options is nil when the setting has no options or sets options: null.
	options map[string]any `clone:"shared"`
}

// NewSetting parses a json.RawMessage option as a JSON object. It encodes any
// other value with the configuration encoder, which names struct fields by
// their yaml tags and ignores json tags and MarshalJSON methods.
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

func (s Setting) Level() Level { return s.level }

func (s Setting) WithLevel(level Level) Setting {
	s.level = level
	return s
}

func (s Setting) HasOptions() bool { return s.options != nil }

// LoadOptions reports false and leaves dst unchanged when the setting has no options.
func (s Setting) LoadOptions(dst any) (bool, error) {
	if s.options == nil {
		return false, nil
	}
	encoded, err := configdecode.Encode(s.options)
	if err != nil {
		return false, err
	}
	if err := configdecode.Decode(encoded, dst); err != nil {
		return false, err
	}
	return true, nil
}

func encodeOptions(options any) (any, error) {
	if raw, ok := options.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return nil, errors.New("json.RawMessage options are not valid JSON")
		}
		parsed, err := document.ParseConfigDocument("options.json", raw)
		if err != nil {
			return nil, err
		}
		return parsed.Value(), nil
	}
	return configdecode.Encode(options)
}

func parseSetting(value any) (Setting, error) {
	var next Setting
	if object, ok := value.(map[string]any); ok {
		for key := range object {
			if key != "level" && key != "options" {
				return Setting{}, configdecode.At(key, fmt.Errorf("unknown rule setting key %q", key))
			}
		}
		level, present := object["level"]
		if !present {
			return Setting{}, errors.New("rule setting requires level")
		}
		if err := next.level.DecodeConfig(level); err != nil {
			return Setting{}, configdecode.At("level", err)
		}
		if options := object["options"]; options != nil {
			mapping, ok := options.(map[string]any)
			if !ok {
				return Setting{}, configdecode.At("options", errors.New("options must be a mapping"))
			}
			encoded, err := configdecode.Encode(mapping)
			if err != nil {
				return Setting{}, err
			}
			encodedMapping, ok := encoded.(map[string]any)
			if !ok {
				return Setting{}, errors.New("encoded rule options must be a mapping")
			}
			next.options = encodedMapping
		}
	} else if err := next.level.DecodeConfig(value); err != nil {
		return Setting{}, err
	}
	return next, nil
}

func (s *Setting) DecodeConfig(value any) error {
	next, err := parseSetting(value)
	if err != nil {
		return err
	}
	*s = next
	return nil
}

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

//go:generate go run ../../../development/generators/deep-copies -root ../../../.. -package ./internal/input/configuration/ruleconfig -type Group -output internal/input/configuration/ruleconfig/deep_copy_group.gen.go

type Group struct {
	preset Preset
	level  Level
	rules  map[string]Setting
}

type groupWire struct {
	Preset Preset             `yaml:"preset,omitempty"`
	Level  Level              `yaml:"level,omitempty"`
	Rules  map[string]Setting `yaml:",inline"`
}

func NewGroup(preset Preset, level Level, rules map[string]Setting) Group {
	return Group{preset: preset, level: level, rules: maps.Clone(rules)}
}

func (g Group) Preset() Preset { return g.preset }

func (g Group) Level() Level { return g.level }

func (g Group) Rule(name string) (Setting, bool) {
	setting, ok := g.rules[name]
	return setting, ok
}

// IsZero lets the shared encoder apply omitempty to fields with private state.
func (g Group) IsZero() bool { return g.preset == "" && g.level == "" && len(g.rules) == 0 }

// Rules yields each rule with an explicit setting, in no particular order.
func (g Group) Rules() iter.Seq2[string, Setting] { return maps.All(g.rules) }

func (g *Group) DecodeConfig(value any) error {
	var next Group
	if object, ok := value.(map[string]any); ok {
		next.rules = make(map[string]Setting)
		for key, entry := range object {
			switch key {
			case "preset":
				if err := next.preset.DecodeConfig(entry); err != nil {
					return configdecode.At(key, err)
				}
			case "level":
				if err := next.level.DecodeConfig(entry); err != nil {
					return configdecode.At(key, err)
				}
			default:
				var setting Setting
				if err := setting.DecodeConfig(entry); err != nil {
					return configdecode.At(key, err)
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

func (g Group) EncodeConfig() (any, error) {
	if g.level != "" && g.preset == "" && len(g.rules) == 0 {
		return string(g.level), nil
	}
	return groupWire{Preset: g.preset, Level: g.level, Rules: maps.Clone(g.rules)}, nil
}

func (g Group) CopyConfig() (any, error) { return deepCopyGroup(g), nil }

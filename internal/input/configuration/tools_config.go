package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

// ToolsConfig configures tool providers, keyed by the rule name each provider
// implements. Bundled providers and providers registered by plugins share it.
type ToolsConfig map[string]ToolConfig

// ToolConfig configures one tool provider.
type ToolConfig struct {
	// Enabled turns the tool on or off. Omission or null keeps it enabled.
	// An explicit empty command or a disabled tool input still disables it.
	Enabled *bool `yaml:"enabled" jsonschema:"nullable,default=true"`
	// Backend selects the execution transport and its settings.
	// Explicit CLI or application invocation settings take precedence.
	Backend *toolconfig.Backend `yaml:"backend,omitempty"`
	// Config is the tool's own configuration: inline settings the provider
	// validates, or a path to the tool's configuration file or directory.
	Config *ToolSettings `yaml:"config"`
}

// ToolSettings is the value of tools.<name>.config: inline settings or a path
// to a configuration file or directory the tool reads itself. Relative paths
// use the directory of the declaring config file; paths supplied by an overlay
// use the analysis working directory.
type ToolSettings struct {
	value     toolSettingsValue
	fromInput bool
}

type (
	toolSettingsValue  interface{ toolSettingsValue() }
	toolSettingsPath   string
	toolSettingsInline map[string]any
)

func (toolSettingsPath) toolSettingsValue()   {}
func (toolSettingsInline) toolSettingsValue() {}

// inlineSettings encodes a settings mapping. Values must be representable in
// JSON, as configuration documents produce them.
func inlineSettings(values map[string]any) (toolSettingsInline, error) {
	if values == nil {
		values = map[string]any{}
	}
	if _, err := json.Marshal(values); err != nil {
		return nil, err
	}
	owned := make(toolSettingsInline, len(values))
	for key, value := range values {
		owned[key] = binding.Native(value)
	}
	return owned, nil
}

func (inline toolSettingsInline) values() map[string]any {
	return cloneObject(map[string]any(inline))
}

// ToolConfigPath selects a configuration file or directory without reading
// it. It rejects empty paths and paths containing newlines or NUL bytes.
func ToolConfigPath(path string) (*ToolSettings, error) {
	if path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("expected a nonempty single-line path")
	}
	return &ToolSettings{value: toolSettingsPath(path)}, nil
}

// ToolConfigInline selects inline settings for the tool provider named tool
// and owns a copy of them. Values must be representable in JSON, as
// configuration documents produce them. A bundled provider's configuration
// schema validates them, as it validates tools.<tool>.config.
func ToolConfigInline(tool string, values map[string]any) (*ToolSettings, error) {
	return validatedToolSettings(tool, toolProviders()[tool], values)
}

// validatedToolSettings validates inline settings against the provider's
// configuration schema, when it declares one, and selects them.
func validatedToolSettings(name string, known toolProvider, values map[string]any) (*ToolSettings, error) {
	if values == nil {
		values = map[string]any{}
	}
	document, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("tools.%s.config: %w", name, err)
	}
	if known.schema != nil {
		if err := known.schema.Validate(document); err != nil {
			return nil, binding.Context(fmt.Sprintf("tools.%s.config", name), schemaBindingError(err))
		}
	}
	inline, err := inlineSettings(values)
	if err != nil {
		return nil, fmt.Errorf("tools.%s.config: %w", name, err)
	}
	return &ToolSettings{value: inline}, nil
}

// Path returns the selected configuration file or directory, or "" for
// inline settings.
func (s *ToolSettings) Path() string {
	if s != nil {
		if path, ok := s.value.(toolSettingsPath); ok {
			return string(path)
		}
	}
	return ""
}

// Inline returns a copy of the inline settings, or nil when a path is selected.
func (s *ToolSettings) Inline() map[string]any {
	if s != nil {
		if inline, ok := s.value.(toolSettingsInline); ok {
			return inline.values()
		}
	}
	return nil
}

// FromInput reports whether an overlay input supplied the selection.
func (s *ToolSettings) FromInput() bool {
	return s != nil && s.fromInput
}

// EncodeConfig preserves the selected path or inline mapping in resolved output.
func (s *ToolSettings) EncodeConfig() (any, error) {
	if s == nil {
		return nil, nil
	}
	switch value := s.value.(type) {
	case toolSettingsPath:
		return string(value), nil
	case toolSettingsInline:
		return value.values(), nil
	default:
		return nil, nil
	}
}

// DecodeConfig decodes each entry with its provider's shorthands and
// configuration schema. Entry names are bound to registered providers later,
// by ValidateRuleNames, as lint.rules.plugins names are.
func (tools *ToolsConfig) DecodeConfig(value any) error {
	if value == nil {
		*tools = nil
		return nil
	}
	entries, ok := value.(map[string]any)
	if !ok {
		return errors.New("tools must be a mapping")
	}
	next := make(ToolsConfig, len(entries))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		tool, err := decodeToolConfig(name, entries[name])
		if err != nil {
			return binding.At(name, err)
		}
		next[name] = tool
	}
	*tools = next
	return nil
}

func decodeToolConfig(name string, value any) (ToolConfig, error) {
	if value == nil {
		return ToolConfig{}, nil
	}
	if enabled, ok := value.(bool); ok {
		return ToolConfig{Enabled: &enabled}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ToolConfig{}, fmt.Errorf("tools.%s must be a boolean or a mapping", name)
	}
	known, bundled := toolProviders()[name]
	normalized, err := foldToolShorthands(name, object, known.shorthands)
	if err != nil {
		return ToolConfig{}, err
	}
	fields, ok := normalized.(map[string]any)
	if !ok {
		return ToolConfig{}, errors.New("normalized tool entry must be a mapping")
	}
	var tool ToolConfig
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		switch key {
		case "enabled":
			err = binding.Decode(value, &tool.Enabled)
		case "backend":
			err = binding.Decode(value, &tool.Backend)
		case "config":
			tool.Config, err = decodeToolSettings(name, value, known, bundled)
		default:
			return ToolConfig{}, binding.At(key, fmt.Errorf("unknown key %q in tools.%s", key, name))
		}
		if err != nil {
			return ToolConfig{}, binding.At(key, err)
		}
	}
	return tool, nil
}

// decodeToolSettings decodes tools.<name>.config. A bundled provider's schema
// validates inline settings, and a path is accepted only for providers that
// read configuration files. Other providers validate their settings when the
// engine loads.
func decodeToolSettings(name string, value any, known toolProvider, bundled bool) (*ToolSettings, error) {
	switch selected := value.(type) {
	case nil:
		return nil, nil
	case string:
		if bundled && !known.configFiles {
			return nil, fmt.Errorf("tools.%s.config must be a mapping; this tool reads no configuration files", name)
		}
		return ToolConfigPath(selected)
	case map[string]any:
		return validatedToolSettings(name, known, selected)
	default:
		return nil, fmt.Errorf("tools.%s.config must be a path or mapping", name)
	}
}

// foldToolShorthands applies each provider's declared property paths and precedence.
func foldToolShorthands(name string, value any, shorthands map[string]configschema.Shorthand) (any, error) {
	if !isMapping(value) {
		return value, nil
	}
	next := cloneMapping(value)
	config := field(value, "config")
	var selected []string
	for key := range members(value) {
		if _, ok := shorthands[key]; ok {
			selected = append(selected, key)
		}
	}
	if len(selected) == 0 {
		return value, nil
	}
	slices.Sort(selected)
	merged := any(mapping())
	if config != nil && !isNull(config) {
		if !isMapping(config) {
			return nil, binding.At(selected[0], fmt.Errorf("tools.%s.%s cannot be combined with a configuration file path", name, selected[0]))
		}
		merged = config
	}
	for _, key := range selected {
		shorthand := shorthands[key]
		child := members(value)[key]
		replace := shorthand.Precedence == configschema.PrecedenceShorthand && child != nil
		var err error
		merged, err = foldShorthand(merged, shorthand.Path, 0, child, replace)
		if err != nil {
			return nil, binding.At(key, fmt.Errorf("tools.%s.%s: %w", name, key, err))
		}
		removeField(next, key)
	}
	setField(next, "config", merged)
	return next, nil
}

func foldShorthand(value any, path []string, depth int, selected any, replace bool) (any, error) {
	next := cloneMapping(value)
	previous := field(value, path[depth])
	if previous == nil {
		setField(next, path[depth], shorthandMapping(path[depth+1:], selected))
		return next, nil
	}
	switch {
	case depth == len(path)-1:
		if replace || isNull(previous) {
			setField(next, path[depth], selected)
		}
	case isNull(previous):
		setField(next, path[depth], shorthandMapping(path[depth+1:], selected))
	case isMapping(previous):
		nested, err := foldShorthand(previous, path, depth+1, selected, replace)
		if err != nil {
			return nil, err
		}
		setField(next, path[depth], nested)
	default:
		return nil, fmt.Errorf("config.%s must be a mapping to hold it", strings.Join(path[:depth+1], "."))
	}
	return next, nil
}

func shorthandMapping(path []string, value any) any {
	if len(path) == 0 {
		return value
	}
	return map[string]any{path[0]: shorthandMapping(path[1:], value)}
}

func normalizeToolSwitch(value any) any {
	if !isMapping(value) {
		return value
	}
	next := cloneMapping(value)
	if tools := field(value, "tools"); isMapping(tools) {
		entries := cloneMapping(tools)
		for name, entry := range members(tools) {
			setField(entries, name, normalizeToolEntry(name, entry))
		}
		setField(next, "tools", entries)
	}
	return next
}

func normalizeToolEntry(name string, value any) any {
	if isBool(value) {
		return map[string]any{"enabled": value}
	}
	if !isMapping(value) {
		return value
	}
	if folded, err := foldToolShorthands(name, value, toolProviders()[name].shorthands); err == nil {
		value = folded
	}
	next := cloneMapping(value)
	if backend := field(value, "backend"); backend != nil {
		setField(next, "backend", normalizeBackendSelection(backend))
	}
	return next
}

func normalizeBackendSelection(value any) any {
	if !isMapping(value) {
		return value
	}
	_, process := members(value)["process"]
	_, wasm := members(value)["wasm"]
	if process == wasm {
		return value
	}
	next := cloneMapping(value)
	if process {
		setField(next, "wasm", nil)
	} else {
		setField(next, "process", nil)
	}
	return next
}

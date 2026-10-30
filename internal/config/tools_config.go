package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"

	"github.com/kjanat/go-yaml/v4"
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
	toolSettingsValue interface{ toolSettingsValue() }
	toolSettingsPath  string
	// toolSettingsInline is the YAML encoding of an inline settings mapping.
	// Holding the encoding, not decoded values, means no caller can alias or
	// mutate shared settings: every read decodes a fresh copy.
	toolSettingsInline []byte
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
	document, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}
	return toolSettingsInline(document), nil
}

// values decodes a fresh copy of the settings. The encoding was produced by
// inlineSettings, so a decoding failure is a defect in this package.
func (inline toolSettingsInline) values() map[string]any {
	values := map[string]any{}
	if err := yaml.Unmarshal(inline, &values); err != nil {
		panic("decode inline tool settings: " + err.Error())
	}
	return values
}

// ToolConfigPath selects a configuration file or directory without reading
// it. It rejects empty paths and paths containing newlines or NUL bytes.
func ToolConfigPath(path string) (*ToolSettings, error) {
	if path == "" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errors.New("expected a nonempty single-line path")
	}
	return &ToolSettings{value: toolSettingsPath(path)}, nil
}

// ToolConfigInline selects inline settings and owns a copy of them. Values
// must be representable in JSON, as configuration documents produce them.
func ToolConfigInline(values map[string]any) (*ToolSettings, error) {
	inline, err := inlineSettings(values)
	if err != nil {
		return nil, fmt.Errorf("inline tool settings: %w", err)
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

// MarshalYAML preserves the selected path or inline mapping in resolved output.
func (s *ToolSettings) MarshalYAML() (any, error) {
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

// UnmarshalYAML decodes each entry with its provider's shorthands and
// configuration schema. Entry names are bound to registered providers later,
// by ValidateRuleNames, as lint.rules.plugins names are.
func (tools *ToolsConfig) UnmarshalYAML(node *yaml.Node) error {
	var entries map[string]yaml.Node
	if err := node.Decode(&entries); err != nil {
		return err
	}
	decoded := make(ToolsConfig, len(entries))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[name]
		tool, err := decodeToolConfig(name, &entry)
		if err != nil {
			return err
		}
		decoded[name] = tool
	}
	*tools = decoded
	return nil
}

func dealias(node *yaml.Node) *yaml.Node {
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}

// decodeToolConfig decodes tools.<name>: null, the boolean shorthand for
// enabled, or a mapping of host keys and provider shorthands.
func decodeToolConfig(name string, node *yaml.Node) (ToolConfig, error) {
	node = dealias(node)
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null" {
		return ToolConfig{}, nil
	}
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!bool" {
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return ToolConfig{}, err
		}
		return ToolConfig{Enabled: &enabled}, nil
	}
	if node.Kind != yaml.MappingNode {
		return ToolConfig{}, fmt.Errorf("tools.%s must be a boolean or a mapping at line %d", name, node.Line)
	}
	provider, bundled := toolProviders()[name]
	node, err := foldToolShorthands(name, node, provider.shorthands)
	if err != nil {
		return ToolConfig{}, err
	}
	var fields map[string]yaml.Node
	if err := node.Decode(&fields); err != nil {
		return ToolConfig{}, err
	}
	var tool ToolConfig
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		field := fields[key]
		value := dealias(&field)
		switch key {
		case "enabled":
			if value.ShortTag() != "!!bool" && value.ShortTag() != "!!null" {
				return ToolConfig{}, fmt.Errorf("tools.%s: enabled must be a boolean at line %d", name, value.Line)
			}
			err = value.Decode(&tool.Enabled)
		case "backend":
			err = value.Decode(&tool.Backend)
		case "config":
			tool.Config, err = decodeToolSettings(name, value, provider, bundled)
		default:
			return ToolConfig{}, fmt.Errorf("yaml: unknown key %q in tools.%s at line %d", key, name, value.Line)
		}
		if err != nil {
			return ToolConfig{}, err
		}
	}
	return tool, nil
}

// decodeToolSettings decodes tools.<name>.config. A bundled provider's schema
// validates inline settings, and a path is accepted only for providers that
// read configuration files. Other providers validate their settings when the
// engine loads.
func decodeToolSettings(name string, node *yaml.Node, provider toolProvider, bundled bool) (*ToolSettings, error) {
	switch {
	case node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null":
		return nil, nil
	case node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str":
		if bundled && !provider.configFiles {
			return nil, fmt.Errorf("tools.%s.config must be a mapping; this tool reads no configuration files", name)
		}
		settings, err := ToolConfigPath(node.Value)
		if err != nil {
			return nil, fmt.Errorf("tools.%s.config: %w", name, err)
		}
		return settings, nil
	case node.Kind == yaml.MappingNode:
		var values map[string]any
		if err := node.Decode(&values); err != nil {
			return nil, err
		}
		if values == nil {
			values = map[string]any{}
		}
		document, err := json.Marshal(values)
		if err != nil {
			return nil, fmt.Errorf("tools.%s.config: %w", name, err)
		}
		if provider.schema != nil {
			if err := provider.schema.Validate(document); err != nil {
				return nil, fmt.Errorf("tools.%s.config: %w", name, err)
			}
		}
		inline, err := inlineSettings(values)
		if err != nil {
			return nil, fmt.Errorf("tools.%s.config: %w", name, err)
		}
		return &ToolSettings{value: inline}, nil
	default:
		return nil, fmt.Errorf("tools.%s.config must be a path or mapping at line %d", name, node.Line)
	}
}

// foldToolShorthands moves provider shorthand keys of a tools.<name> mapping
// into its config mapping. It returns node itself when there is nothing to
// fold, and an error when a shorthand meets a path or repeats a config key.
func foldToolShorthands(name string, node *yaml.Node, shorthands []string) (*yaml.Node, error) {
	var folded []*yaml.Node
	rest := make([]*yaml.Node, 0, len(node.Content))
	var config *yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch {
		case slices.Contains(shorthands, key.Value):
			folded = append(folded, key, value)
		case key.Value == "config":
			config = dealias(value)
			rest = append(rest, key, value)
		default:
			rest = append(rest, key, value)
		}
	}
	if len(folded) == 0 {
		return node, nil
	}
	merged := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: node.Line, Column: node.Column}
	if config != nil {
		switch {
		case config.Kind == yaml.MappingNode:
			merged.Content = slices.Clone(config.Content)
			merged.Line, merged.Column = config.Line, config.Column
		case config.Kind != yaml.ScalarNode || config.ShortTag() != "!!null":
			return nil, fmt.Errorf("tools.%s.%s cannot be combined with a configuration file path at line %d", name, folded[0].Value, folded[0].Line)
		}
	}
	for i := 0; i < len(folded); i += 2 {
		for j := 0; j+1 < len(merged.Content); j += 2 {
			if merged.Content[j].Value == folded[i].Value {
				return nil, fmt.Errorf("tools.%s.%s duplicates tools.%s.config.%s at line %d", name, folded[i].Value, name, folded[i].Value, folded[i].Line)
			}
		}
		merged.Content = append(merged.Content, folded[i], folded[i+1])
	}
	normalized := *node
	normalized.Content = rest
	if config == nil {
		normalized.Content = append(normalized.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "config"}, merged)
	} else {
		for i := 0; i+1 < len(normalized.Content); i += 2 {
			if normalized.Content[i].Value == "config" {
				normalized.Content[i+1] = merged
			}
		}
	}
	return &normalized, nil
}

// normalizeToolSwitch rewrites every tools.<name> entry before merging, so a
// partial setting overrides only what it names: the boolean shorthand becomes
// {enabled: ...}, provider shorthands fold into config, and selecting one
// backend transport resets the other.
func normalizeToolSwitch(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		normalized := *node
		normalized.Content = []*yaml.Node{normalizeToolSwitch(node.Content[0])}
		return &normalized
	}
	if node.Kind != yaml.MappingNode {
		return node
	}
	normalized := *node
	normalized.Content = slices.Clone(node.Content)
	for i := 0; i+1 < len(normalized.Content); i += 2 {
		tools := normalized.Content[i+1]
		if normalized.Content[i].Value != "tools" || tools.Kind != yaml.MappingNode {
			continue
		}
		entries := *tools
		entries.Content = slices.Clone(tools.Content)
		for j := 0; j+1 < len(entries.Content); j += 2 {
			entries.Content[j+1] = normalizeToolEntry(entries.Content[j].Value, entries.Content[j+1])
		}
		normalized.Content[i+1] = &entries
	}
	return &normalized
}

func normalizeToolEntry(name string, entry *yaml.Node) *yaml.Node {
	if entry.Kind == yaml.ScalarNode && entry.Tag == "!!bool" {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: entry.Line, Column: entry.Column, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled", Line: entry.Line, Column: entry.Column}, entry,
		}}
	}
	if entry.Kind != yaml.MappingNode {
		return entry
	}
	// A conflicting shorthand stays in place for decoding to report.
	if folded, err := foldToolShorthands(name, entry, toolProviders()[name].shorthands); err == nil {
		entry = folded
	}
	normalized := *entry
	normalized.Content = slices.Clone(entry.Content)
	for i := 0; i+1 < len(normalized.Content); i += 2 {
		if normalized.Content[i].Value == "backend" {
			normalized.Content[i+1] = normalizeBackendSelection(normalized.Content[i+1])
		}
	}
	return &normalized
}

// normalizeBackendSelection adds a null for the transport a backend mapping
// does not select, so merging it replaces an inherited selection.
func normalizeBackendSelection(backend *yaml.Node) *yaml.Node {
	if backend.Kind != yaml.MappingNode {
		return backend
	}
	selected := ""
	for i := 0; i+1 < len(backend.Content); i += 2 {
		if backend.Content[i].Value == "process" || backend.Content[i].Value == "wasm" {
			if selected != "" {
				return backend
			}
			selected = backend.Content[i].Value
		}
	}
	if selected == "" {
		return backend
	}
	other := "wasm"
	if selected == "wasm" {
		other = "process"
	}
	normalized := *backend
	normalized.Content = append(slices.Clone(backend.Content), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: other}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"})
	return &normalized
}

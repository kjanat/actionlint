package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"

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
			return nil, fmt.Errorf("tools.%s.config: %w", name, err)
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
	known, bundled := toolProviders()[name]
	node, err := foldToolShorthands(name, node, known.shorthands)
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
			tool.Config, err = decodeToolSettings(name, value, known, bundled)
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
func decodeToolSettings(name string, node *yaml.Node, known toolProvider, bundled bool) (*ToolSettings, error) {
	switch {
	case node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null":
		return nil, nil
	case node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str":
		if bundled && !known.configFiles {
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
		return validatedToolSettings(name, known, values)
	default:
		return nil, fmt.Errorf("tools.%s.config must be a path or mapping at line %d", name, node.Line)
	}
}

// foldToolShorthands moves provider shorthand keys of a tools.<name> mapping
// to the config properties they set. When the mapping also sets a property
// under config, the shorthand's precedence picks the value kept. It returns
// node itself when there is nothing to fold, and an error when a shorthand
// meets a configuration file path or a property path through a non-mapping.
func foldToolShorthands(name string, node *yaml.Node, shorthands map[string]configschema.Shorthand) (*yaml.Node, error) {
	var folded []*yaml.Node
	rest := make([]*yaml.Node, 0, len(node.Content))
	var config *yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		_, shorthand := shorthands[key.Value]
		switch {
		case shorthand:
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
			merged = config
		case !isNull(config):
			return nil, fmt.Errorf("tools.%s.%s cannot be combined with a configuration file path at line %d", name, folded[0].Value, folded[0].Line)
		}
	}
	for i := 0; i < len(folded); i += 2 {
		key, value := folded[i], folded[i+1]
		shorthand := shorthands[key.Value]
		replace := shorthand.Precedence == configschema.PrecedenceShorthand && !isNull(dealias(value))
		var err error
		if merged, err = foldShorthand(merged, shorthand.Path, 0, value, replace); err != nil {
			return nil, fmt.Errorf("tools.%s.%s at line %d: %w", name, key.Value, key.Line, err)
		}
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

func isNull(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null"
}

// foldShorthand returns a copy of mapping with the property at path[depth:]
// set to value. An absent or null property is always set; an existing value
// is replaced only when replace is true. Missing mappings along the path are
// created, and mapping itself is never modified.
func foldShorthand(mapping *yaml.Node, path []string, depth int, value *yaml.Node, replace bool) (*yaml.Node, error) {
	folded := *mapping
	folded.Content = slices.Clone(mapping.Content)
	for i := 0; i+1 < len(folded.Content); i += 2 {
		if folded.Content[i].Value != path[depth] {
			continue
		}
		existing := dealias(folded.Content[i+1])
		switch {
		case depth == len(path)-1:
			if replace || isNull(existing) {
				folded.Content[i+1] = value
			}
		case isNull(existing):
			folded.Content[i+1] = shorthandMapping(path[depth+1:], value)
		case existing.Kind == yaml.MappingNode:
			nested, err := foldShorthand(existing, path, depth+1, value, replace)
			if err != nil {
				return nil, err
			}
			folded.Content[i+1] = nested
		default:
			return nil, fmt.Errorf("config.%s must be a mapping to hold it", strings.Join(path[:depth+1], "."))
		}
		return &folded, nil
	}
	folded.Content = append(folded.Content, shorthandKey(path[depth], value), shorthandMapping(path[depth+1:], value))
	return &folded, nil
}

// shorthandMapping nests value under path, one mapping per property name.
func shorthandMapping(path []string, value *yaml.Node) *yaml.Node {
	if len(path) == 0 {
		return value
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: value.Line, Column: value.Column, Content: []*yaml.Node{
		shorthandKey(path[0], value), shorthandMapping(path[1:], value),
	}}
}

func shorthandKey(name string, value *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name, Line: value.Line, Column: value.Column}
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

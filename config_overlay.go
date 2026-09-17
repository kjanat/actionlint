package actionlint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4"
)

// ConfigKeys returns the supported top-level configuration keys in declaration order.
// Action inputs use these same names and YAML types.
func ConfigKeys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		if name := f.Tag.Get("yaml"); name != "" && name != "-" {
			keys = append(keys, strings.Split(name, ",")[0])
		}
	}
	return keys
}

// ConfigOverlay is a validated, immutable configuration override. Construct it
// with ParseConfigOverlay; the original YAML nodes preserve null, false and [].
type ConfigOverlay struct {
	name string
	node *yaml.Node
}

// ConfigOverlayError reports individually valid inputs whose combined settings
// are invalid, for example timeout bounds that conflict with the config file.
type ConfigOverlayError struct {
	inputs []string
	cause  error
}

func (e *ConfigOverlayError) Error() string {
	return fmt.Sprintf("configuration after inputs %s: %v", strings.Join(e.inputs, ", "), e.cause)
}

// Unwrap returns the configuration validation error.
func (e *ConfigOverlayError) Unwrap() error { return e.cause }

// ParseConfigOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. Unknown keys are rejected here;
// existing config files retain their parsing behavior.
func ParseConfigOverlay(input string, content []byte) (ConfigOverlay, error) {
	var doc yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(content))
	if err := d.Decode(&doc); err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(doc.Content) != 1 {
		return ConfigOverlay{}, fmt.Errorf("input %s: expected one YAML or JSON value", input)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ConfigOverlay{}, fmt.Errorf("input %s: expected one YAML or JSON document", input)
	}
	node := doc.Content[0]
	if input != "config" {
		known := false
		for _, key := range ConfigKeys() {
			known = known || key == input
		}
		if !known {
			return ConfigOverlay{}, fmt.Errorf("unknown configuration input %q", input)
		}
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: input}, node,
		}}
	}
	if _, err := decodeOverlayConfig(node, true); err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node, err := expandConfigNode(node, make(map[*yaml.Node]bool))
	if err != nil {
		return ConfigOverlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	return ConfigOverlay{input, node}, nil
}

// Resolve aliases and YAML merge keys before overlaying. Otherwise replacing an
// anchor's value can invalidate aliases elsewhere in an unchanged section.
func expandConfigNode(node *yaml.Node, visiting map[*yaml.Node]bool) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if visiting[node] {
		return nil, fmt.Errorf("cyclic YAML alias at line %d", node.Line)
	}
	visiting[node] = true
	defer delete(visiting, node)
	if node.Kind == yaml.AliasNode {
		return expandConfigNode(node.Alias, visiting)
	}
	expandedNode := *node
	expandedNode.Anchor = ""
	expandedNode.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		expanded, err := expandConfigNode(child, visiting)
		if err != nil {
			return nil, err
		}
		expandedNode.Content[i] = expanded
	}
	if expandedNode.Kind != yaml.MappingNode {
		return &expandedNode, nil
	}
	entries := make([]*yaml.Node, 0, len(expandedNode.Content))
	keys := make(map[string]bool)
	var inherited []*yaml.Node
	for i := 0; i < len(expandedNode.Content); i += 2 {
		key, value := expandedNode.Content[i], expandedNode.Content[i+1]
		if key.ShortTag() != "!!merge" {
			entries = append(entries, key, value)
			keys[key.Value] = true
			continue
		}
		if value.Kind == yaml.SequenceNode {
			inherited = append(inherited, value.Content...)
		} else {
			inherited = append(inherited, value)
		}
	}
	// Explicit keys win; in a merge sequence, the first mapping wins.
	for _, mapping := range inherited {
		if mapping.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("YAML merge must contain mappings at line %d", mapping.Line)
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			key, value := mapping.Content[i], mapping.Content[i+1]
			if !keys[key.Value] {
				entries = append(entries, key, value)
				keys[key.Value] = true
			}
		}
	}
	expandedNode.Content = entries
	return &expandedNode, nil
}

func decodeOverlayConfig(node *yaml.Node, strict bool) (*Config, error) {
	b, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	if strict {
		d := yaml.NewDecoder(bytes.NewReader(b))
		d.KnownFields(true)
		var c Config
		if err := d.Decode(&c); err != nil {
			return nil, err
		}
	}
	return ParseConfig(b)
}

// mergeConfigNodes returns a fresh mapping, never mutating a loaded config or
// an input. Lists/scalars replace; explicit null resets; empty maps inherit.
func mergeConfigNodes(base, overlay *yaml.Node) *yaml.Node {
	for base != nil && base.Kind == yaml.AliasNode {
		base = base.Alias
	}
	for overlay.Kind == yaml.AliasNode {
		overlay = overlay.Alias
	}
	if base == nil || base.Kind != yaml.MappingNode || overlay.Kind != yaml.MappingNode {
		return overlay
	}
	merged := *base
	merged.Content = append([]*yaml.Node{}, base.Content...)
	for i := 0; i < len(overlay.Content); i += 2 {
		key, value := overlay.Content[i], overlay.Content[i+1]
		found := false
		for j := 0; j < len(merged.Content); j += 2 {
			if merged.Content[j].Value == key.Value {
				merged.Content[j+1] = mergeConfigNodes(merged.Content[j+1], value)
				found = true
				break
			}
		}
		if !found {
			merged.Content = append(merged.Content, key, value)
		}
	}
	return &merged
}

type loadedConfig struct {
	config   *Config
	node     *yaml.Node
	filename string
}

// ConfigReport describes the configuration actually selected for one project.
type ConfigReport struct {
	Project   string
	File      string
	Explicit  bool
	Overrides []string
}

type linterConfigState struct {
	sync.Mutex
	source   *loadedConfig
	overlays []ConfigOverlay
	onLoaded func(ConfigReport)
	loaded   map[*Project]*Config
}

func (l *Linter) configForProject(project *Project) (*Config, error) {
	var cfg *Config
	var source *loadedConfig
	if l.defaultConfig != nil {
		cfg = l.defaultConfig
		if l.configState != nil {
			source = l.configState.source
		}
	} else if project != nil {
		cfg, source = project.Config(), project.config
	}
	s := l.configState
	if s == nil {
		return cfg, nil
	}
	s.Lock()
	defer s.Unlock()
	if loaded, ok := s.loaded[project]; ok {
		return loaded, nil
	}
	report := ConfigReport{Explicit: l.defaultConfig != nil}
	var node *yaml.Node
	if project != nil {
		report.Project = project.RootDir()
	}
	if source != nil {
		node, report.File = source.node, source.filename
	}
	if len(s.overlays) > 0 {
		expanded, err := expandConfigNode(node, make(map[*yaml.Node]bool))
		if err != nil {
			return nil, fmt.Errorf("configuration %s: %w", report.File, err)
		}
		node = expanded
	}
	for _, overlay := range s.overlays {
		node = mergeConfigNodes(node, overlay.node)
		report.Overrides = append(report.Overrides, overlay.name)
	}
	if len(s.overlays) > 0 {
		var err error
		cfg, err = decodeOverlayConfig(node, false)
		if err != nil {
			return nil, &ConfigOverlayError{report.Overrides, err}
		}
	}
	s.loaded[project] = cfg
	if s.onLoaded != nil {
		s.onLoaded(report)
	}
	return cfg, nil
}

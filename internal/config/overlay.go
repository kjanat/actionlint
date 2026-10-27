package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"strings"

	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"github.com/kjanat/go-yaml/v4"
)

// Keys returns the supported top-level configuration keys in declaration order.
// Overlay documents use these names and YAML types.
func Keys() []string {
	t := reflect.TypeFor[Config]()
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		if name := f.Tag.Get("yaml"); name != "" && name != "-" {
			keys = append(keys, strings.Split(name, ",")[0])
		}
	}
	return keys
}

// Overlay is a validated, immutable configuration override. Construct it
// with ParseOverlay; the original YAML nodes preserve null, false and [].
type Overlay struct {
	name string
	node *yaml.Node
}

// OverlayError reports individually valid inputs whose combined settings
// are invalid, for example timeout bounds that conflict with the config file.
type OverlayError struct {
	inputs []string
	cause  error
}

func (e *OverlayError) Error() string {
	return fmt.Sprintf("configuration after inputs %s: %v", strings.Join(e.inputs, ", "), e.cause)
}

// Unwrap returns the configuration validation error.
func (e *OverlayError) Unwrap() error { return e.cause }

// ParseOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. It rejects unknown keys.
func ParseOverlay(input string, content []byte, additional ...string) (Overlay, error) {
	return parseOverlay(input, content, true, additional...)
}

// ParseOverlayBeforePlugins validates input structure and settings before plugin
// rule names are available. Normal configuration resolution binds those names
// after the selected plugins have loaded.
func ParseOverlayBeforePlugins(input string, content []byte) (Overlay, error) {
	return parseOverlay(input, content, false)
}

func parseOverlay(input string, content []byte, bindRules bool, additional ...string) (Overlay, error) {
	var doc yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(content))
	if err := d.Decode(&doc); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(doc.Content) != 1 {
		return Overlay{}, fmt.Errorf("input %s: expected one YAML or JSON value", input)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return Overlay{}, fmt.Errorf("input %s: expected one YAML or JSON document", input)
	}
	node := doc.Content[0]
	if input != "config" {
		known := false
		for _, key := range append(Keys(), legacyKeys...) {
			known = known || key == input
		}
		if !known {
			return Overlay{}, fmt.Errorf("unknown configuration input %q", input)
		}
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: input}, node,
		}}
	}
	if warnings := configWarnings(node, nil); len(warnings) != 0 {
		return Overlay{}, fmt.Errorf("input %s: %s", input, warnings[0].Message)
	}
	var err error
	node, err = NormalizeForMerge(node)
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	var config Config
	if err := node.Load(&config, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(config.Extends) > 0 {
		return Overlay{}, fmt.Errorf("input %s: extends requires a config file origin", input)
	}
	if _, err := resolveNode(node, nil, bindRules, additional...); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node, err = configtree.Expand(node, make(map[*yaml.Node]bool))
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "local-actions" {
				normalizeLocalActionSourceNode(node.Content[i+1])
			}
		}
	}
	return Overlay{input, normalizeToolSwitch(node)}, nil
}

// ResolveBeforePlugins merges already parsed overlays for dependency selection.
// Only rule-name binding is deferred; it is repeated by the analysis session.
func ResolveBeforePlugins(node *yaml.Node, overlays []Overlay) (Resolved, error) {
	inputs := make(map[*yaml.Node]Input)
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		node = configtree.Merge(node, overlay.node, inputs)
		names = append(names, overlay.name)
	}
	resolved, err := resolveNode(node, nil, false)
	if err != nil && len(names) != 0 {
		return Resolved{}, &OverlayError{names, err}
	}
	return resolved, err
}

type Loaded struct {
	Resolved
	Filename string
}

type Input = configtree.Input

func MarkInput(node *yaml.Node, input string, inputs map[*yaml.Node]Input) {
	inputs[node] = Input{Name: input}
	for _, child := range node.Content {
		MarkInput(child, input, inputs)
	}
}

// Name returns the input that supplied this overlay.
func (o Overlay) Name() string { return o.name }

// BindPaths returns a copy whose relative tool paths resolve from directory.
func (o Overlay) BindPaths(directory string) (Overlay, error) {
	node, err := configtree.Expand(o.node, make(map[*yaml.Node]bool))
	if err != nil {
		return Overlay{}, err
	}
	toolconfig.NormalizePaths(node, directory)
	return Overlay{o.name, node}, nil
}

// Overlaid merges parsed overlays over a loaded source and resolves the result.
// Without overlays it returns the supplied configuration and the source's document.
func Overlaid(cfg *Config, source *Loaded, overlays []Overlay, additional ...string) (*Config, Resolved, error) {
	var node *yaml.Node
	var resolved Resolved
	if source != nil {
		node, resolved = source.Node, source.Resolved
	}
	inputs := maps.Clone(resolved.Inputs)
	if inputs == nil {
		inputs = make(map[*yaml.Node]Input)
	}
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		MarkInput(overlay.node, overlay.name, inputs)
		node = configtree.Merge(node, overlay.node, inputs)
		names = append(names, overlay.name)
	}
	if len(overlays) > 0 {
		var err error
		resolved, err = ResolveNode(node, inputs, additional...)
		if err != nil {
			return nil, Resolved{}, &OverlayError{names, err}
		}
		cfg = resolved.Config
		if source != nil {
			cfg.filename = absPath(source.Filename)
			cfg.configFiles = source.Config.configFiles
			for i := range resolved.Warnings {
				if resolved.Warnings[i].File == "" {
					resolved.Warnings[i].File = cfg.filename
				}
			}
			resolved.Warnings = MergeWarnings(source.Warnings, resolved.Warnings)
		}
	} else if source == nil {
		var err error
		resolved, err = ResolveNode(nil, nil, additional...)
		if err != nil {
			return nil, Resolved{}, err
		}
	}
	return cfg, resolved, nil
}

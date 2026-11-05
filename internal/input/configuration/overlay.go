package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"

	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
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
	name  string
	node  *yaml.Node
	rules *yaml.Node
}

// OverlayError reports individually valid inputs whose combined settings
// are invalid, for example timeout bounds that conflict with the config file.
type OverlayError = configreport.OverlayError

// ParseOverlay parses a complete YAML/JSON document (input "config") or
// the YAML/JSON value of one ConfigKeys entry. It rejects unknown keys.
func (ruleSet RuleSet) ParseOverlay(input string, content []byte, additional ...string) (Overlay, error) {
	return ruleSet.parseOverlay(input, content, true, additional...)
}

// ParseOverlayBeforePlugins validates input structure and settings before plugin
// rule names are available. Normal configuration resolution binds those names
// after the selected plugins have loaded.
func (ruleSet RuleSet) ParseOverlayBeforePlugins(input string, content []byte) (Overlay, error) {
	return ruleSet.parseOverlay(input, content, false)
}

func (ruleSet RuleSet) parseOverlay(input string, content []byte, bindRules bool, additional ...string) (Overlay, error) {
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
	node, err = ruleSet.NormalizeForMerge(node)
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	var config Config
	if err := ruleSet.DecodeNode(node, &config); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	if len(config.Extends) > 0 {
		return Overlay{}, fmt.Errorf("input %s: extends requires a config file origin", input)
	}
	if _, err := ruleSet.resolveNode(node, nil, bindRules, additional...); err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	node, err = configtree.Expand(node, make(map[*yaml.Node]bool))
	if err != nil {
		return Overlay{}, fmt.Errorf("input %s: %w", input, err)
	}
	rules, err := captureRuleDocument(node)
	if err != nil {
		return Overlay{}, err
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == "local-actions" {
				normalizeLocalActionSourceNode(node.Content[i+1])
			}
			if node.Content[i].Value == "lint" {
				node.Content[i+1], err = ruleSet.normalizeRuleLocations(node.Content[i+1])
				if err != nil {
					return Overlay{}, fmt.Errorf("input %s: %w", input, err)
				}
			}
		}
	}
	return Overlay{name: input, node: normalizeToolSwitch(node), rules: rules}, nil
}

// ResolveBeforePlugins merges already parsed overlays for dependency selection.
// Only rule-name binding is deferred; it is repeated by the analysis session.
func (ruleSet RuleSet) ResolveBeforePlugins(node *yaml.Node, overlays []Overlay) (Resolved, error) {
	inputs := make(map[*yaml.Node]Input)
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		node = MergeDocuments(node, overlay.node, inputs)
		names = append(names, overlay.name)
	}
	resolved, err := ruleSet.resolveNode(node, nil, false)
	if err != nil && len(names) != 0 {
		return Resolved{}, configreport.NewOverlayError(names, err)
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
	rules, err := captureRuleDocument(o.rules)
	if err != nil {
		return Overlay{}, err
	}
	toolconfig.NormalizePaths(rules, directory)
	return Overlay{name: o.name, node: node, rules: rules}, nil
}

// Overlaid merges parsed overlays over a loaded source and resolves the result.
// Without overlays it returns the supplied configuration and the source's document.
func (ruleSet RuleSet) Overlaid(cfg *Config, source *Loaded, overlays []Overlay, additional ...string) (*Config, Resolved, error) {
	var node *yaml.Node
	var resolved Resolved
	var bindingSources []bindingSource
	captured := &ruleSources{}
	if cfg != nil {
		bindingSources = slices.Clone(cfg.Lint.Rules.bindingSources)
		if cfg.Lint.Rules.sources != nil {
			captured = cfg.Lint.Rules.sources
		} else {
			var err error
			baseline, err := ruleSet.EncodeNode(cfg)
			if err != nil {
				return nil, Resolved{}, err
			}
			captured.base, err = captureRuleDocument(baseline)
			if err != nil {
				return nil, Resolved{}, err
			}
		}
	}
	if source != nil {
		node, resolved = source.Node, source.Resolved
	}
	inputs := maps.Clone(resolved.Inputs)
	if inputs == nil {
		inputs = make(map[*yaml.Node]Input)
	}
	names := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		captured = captured.withOverlay(overlay.rules)
		var settings Config
		if err := ruleSet.DecodeNode(overlay.node, &settings); err != nil {
			return nil, Resolved{}, err
		}
		bindingSources = append(bindingSources, ruleBindingSources(overlay.name, &settings)...)
		MarkInput(overlay.node, overlay.name, inputs)
		node = MergeDocuments(node, overlay.node, inputs)
		names = append(names, overlay.name)
	}
	if len(overlays) > 0 {
		var err error
		resolved, err = ruleSet.ResolveNode(node, inputs, additional...)
		if err != nil {
			return nil, Resolved{}, configreport.NewOverlayError(names, err)
		}
		cfg = resolved.Config
		cfg.Lint.Rules.bindingSources = bindingSources
		cfg.Lint.Rules.sources = captured
		if source != nil {
			resolved.Provenance = source.Provenance
			for i := range resolved.Warnings {
				if resolved.Warnings[i].File == "" {
					resolved.Warnings[i].File = source.Provenance.Path()
				}
			}
			resolved.Warnings = MergeWarnings(source.Warnings, resolved.Warnings)
		}
	} else if source == nil {
		var err error
		resolved, err = ruleSet.ResolveNode(nil, nil, additional...)
		if err != nil {
			return nil, Resolved{}, err
		}
	}
	return cfg, resolved, nil
}

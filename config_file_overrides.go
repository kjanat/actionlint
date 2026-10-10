package actionlint

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/internal/configtree"
	"actionlint.kjanat.dev/internal/filefilter"
	"go.yaml.in/yaml/v4"
)

// ConfigOverride scopes settings to selected files. Other feature sections can
// share this scope when formatting and language-server support are introduced.
type ConfigOverride struct {
	// Includes contains repository-relative file paths or doublestar globs.
	// At least one positive pattern is required. Prefix a pattern with ! to exclude
	// matches from this entry; exclusions win regardless of pattern order.
	Includes []string `yaml:"includes" jsonschema:"required,minItems=1"`
	// Excludes removes files from this override; exclusions always win.
	Excludes []string `yaml:"excludes,omitempty" jsonschema:"nullable"`
	// Lint overlays only explicitly supplied lint fields. Null resets lint defaults.
	Lint     *LintConfig `yaml:"lint" jsonschema:"nullable"`
	lintNode *yaml.Node
	// Tools overlays external checker settings for the selected files.
	Tools     *ToolsConfig `yaml:"tools" jsonschema:"nullable"`
	toolsNode *yaml.Node
}

// UnmarshalYAML validates paths and retains omitted/null fields for merging.
func (o *ConfigOverride) UnmarshalYAML(n *yaml.Node) error {
	var err error
	n, err = configtree.Expand(n, make(map[*yaml.Node]bool))
	if err != nil {
		return err
	}
	type plain ConfigOverride
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	positive := false
	for _, pattern := range next.Includes {
		if !filefilter.ValidPattern(pattern) {
			return fmt.Errorf("yaml: invalid repository-relative override glob %q", pattern)
		}
		positive = positive || !strings.HasPrefix(pattern, "!")
	}
	for _, pattern := range next.Excludes {
		if !filefilter.ValidPattern(pattern) {
			return fmt.Errorf("yaml: invalid override exclusion %q", pattern)
		}
	}
	if !positive {
		return errors.New("yaml: overrides.includes requires at least one positive file glob")
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == "lint" {
			next.lintNode = n.Content[i+1]
		}
		if n.Content[i].Value == "tools" {
			next.toolsNode = n.Content[i+1]
		}
		if n.Content[i].Value == "includes" || n.Content[i].Value == "excludes" {
			for _, pattern := range n.Content[i+1].Content {
				if pattern.Kind != yaml.ScalarNode || pattern.ShortTag() != "!!str" {
					return errors.New("yaml: overrides.includes entries must be strings")
				}
			}
		}
	}
	*o = ConfigOverride(next)
	return nil
}

// MarshalYAML preserves omitted fields in partial overlays during round trips.
func (o ConfigOverride) MarshalYAML() (any, error) {
	value := map[string]any{"includes": o.Includes}
	if o.Excludes != nil {
		value["excludes"] = o.Excludes
	}
	if o.lintNode != nil {
		value["lint"] = o.lintNode
	} else if o.Lint != nil {
		value["lint"] = o.Lint
	}
	if o.toolsNode != nil {
		value["tools"] = o.toolsNode
	} else if o.Tools != nil {
		node, err := programmaticToolsNode(o.Tools)
		if err != nil {
			return nil, err
		}
		value["tools"] = node
	}
	return value, nil
}

func (o ConfigOverride) matches(path string) bool {
	return (filefilter.Selection{Includes: o.Includes, Excludes: o.Excludes}).Match(path)
}

func lintConfigNode(config LintConfig) (*yaml.Node, error) {
	if config.source != nil {
		return configtree.Expand(config.source, make(map[*yaml.Node]bool))
	}
	var node yaml.Node
	if err := node.Encode(config); err != nil {
		return nil, err
	}
	return &node, nil
}

// configForFile never mutates shared project settings; parallel file analyses
// each receive their own effective configuration.
func configForFile(config *Config, path, root string) (*Config, error) {
	if config == nil || len(config.Overrides) == 0 {
		return config, nil
	}
	if filepath.IsAbs(path) && root != "" {
		if relative, err := filepath.Rel(absPath(root), path); err == nil {
			path = relative
		}
	}
	path = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
	node, err := lintConfigNode(config.Lint)
	if err != nil {
		return nil, err
	}
	tools := config.Tools
	for _, override := range config.Overrides {
		if !override.matches(path) {
			continue
		}
		if override.toolsNode != nil || override.Tools != nil {
			tools, err = mergeToolsOverride(tools, override)
			if err != nil {
				return nil, err
			}
		}
		overlay := override.lintNode
		if overlay == nil {
			if override.Lint == nil {
				continue
			}
			overlay, err = lintConfigNode(*override.Lint)
			if err != nil {
				return nil, err
			}
		}
		node = configtree.Merge(expandLintShorthands(node), overlay, map[*yaml.Node]configInput{})
	}
	next := *config
	next.Tools = tools
	next.Lint = LintConfig{}
	content, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(content, &next.Lint); err != nil {
		return nil, err
	}
	return &next, nil
}

func mergeToolsOverride(base ToolsConfig, override ConfigOverride) (ToolsConfig, error) {
	var node, overlay yaml.Node
	if err := node.Encode(map[string]any{"tools": base}); err != nil {
		return ToolsConfig{}, err
	}
	var value any
	if override.toolsNode != nil {
		value = override.toolsNode
	} else {
		var err error
		value, err = programmaticToolsNode(override.Tools)
		if err != nil {
			return ToolsConfig{}, err
		}
	}
	if err := overlay.Encode(map[string]any{"tools": value}); err != nil {
		return ToolsConfig{}, err
	}
	merged := configtree.Merge(normalizeToolSwitch(&node), normalizeToolSwitch(&overlay), map[*yaml.Node]configInput{})
	var decoded struct {
		Tools ToolsConfig `yaml:"tools"`
	}
	if err := merged.Decode(&decoded); err != nil {
		return ToolsConfig{}, err
	}
	if decoded.Tools.Shellcheck.Config != nil {
		source := base.Shellcheck.Config
		if override.Tools != nil && override.Tools.Shellcheck.Config != nil {
			source = override.Tools.Shellcheck.Config
		}
		if source != nil {
			decoded.Tools.Shellcheck.Config.fromInput = source.fromInput
		}
	}
	return decoded.Tools, nil
}

// Programmatic nil fields omit settings; parsed nodes retain explicit resets.
func programmaticToolsNode(tools *ToolsConfig) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(tools); err != nil {
		return nil, err
	}
	var prune func(*yaml.Node)
	prune = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			entries := node.Content[:0]
			for i := 0; i < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				if value.ShortTag() != "!!null" {
					prune(value)
					entries = append(entries, key, value)
				}
			}
			node.Content = entries
		} else {
			for _, child := range node.Content {
				prune(child)
			}
		}
	}
	prune(&node)
	return &node, nil
}

// Expand shorthand before partial mappings overlay it, retaining inherited
// group/rule levels and options. Scalar overlays still replace the whole value.
func expandLintShorthands(node *yaml.Node) *yaml.Node {
	for category := range (LintRulesConfig{}).groups() {
		node = expandRuleLevel(node, "rules", category)
		for _, d := range builtinRuleDescriptors() {
			if d.Category == category {
				node = expandRuleLevel(node, "rules", category, d.Name)
			}
		}
	}
	return node
}

func expandRuleLevel(node *yaml.Node, path ...string) *yaml.Node {
	if node == nil {
		return nil
	}
	if len(path) == 0 {
		if node.Kind == yaml.ScalarNode && (node.ShortTag() == "!!str" && node.Value != "default" || node.ShortTag() == "!!bool") {
			return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: node.Line, Column: node.Column, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "level"}, node}}
		}
		return node
	}
	if node.Kind != yaml.MappingNode {
		return node
	}
	expanded := *node
	expanded.Content = append([]*yaml.Node{}, node.Content...)
	for i := 0; i < len(expanded.Content); i += 2 {
		if expanded.Content[i].Value == path[0] {
			expanded.Content[i+1] = expandRuleLevel(expanded.Content[i+1], path[1:]...)
		}
	}
	return &expanded
}

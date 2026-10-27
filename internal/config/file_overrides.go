package config

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"actionlint.kjanat.dev/v2/internal/filefilter"

	"github.com/kjanat/go-yaml/v4"
)

// Override scopes settings to selected files. Other feature sections can
// share this scope when formatting and language-server support are introduced.
type Override struct {
	// Includes contains repository-relative file paths or doublestar globs.
	// At least one positive pattern is required. Prefix a pattern with ! to exclude
	// matches from this entry; exclusions win regardless of pattern order.
	Includes []string `yaml:"includes" jsonschema:"required,minItems=1"`
	// Excludes removes files from this override; exclusions always win.
	Excludes []string `yaml:"excludes,omitempty" jsonschema:"nullable"`
	// Lint overlays only explicitly supplied lint fields. Null resets lint defaults.
	Lint     *LintConfig `yaml:"lint" jsonschema:"nullable"`
	lintNode *yaml.Node
	// LocalActions overlays generated action source mappings for matching files.
	LocalActions    LocalActionSources `yaml:"local-actions" jsonschema:"nullable"`
	localActionsSet bool
}

// UnmarshalYAML validates paths and records which fields were omitted or reset.
func (o *Override) UnmarshalYAML(n *yaml.Node) error {
	var err error
	n, err = configtree.Expand(n, make(map[*yaml.Node]bool))
	if err != nil {
		return err
	}
	type plain Override
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
		if n.Content[i].Value == "local-actions" {
			next.localActionsSet = true
		}
		if n.Content[i].Value == "lint" {
			next.lintNode = n.Content[i+1]
		}
		if n.Content[i].Value == "includes" || n.Content[i].Value == "excludes" {
			for _, pattern := range n.Content[i+1].Content {
				if pattern.Kind != yaml.ScalarNode || pattern.ShortTag() != "!!str" {
					return errors.New("yaml: overrides.includes entries must be strings")
				}
			}
		}
	}
	*o = Override(next)
	return nil
}

// MarshalYAML preserves omitted fields in partial overlays during round trips.
func (o Override) MarshalYAML() (any, error) {
	value := map[string]any{"includes": o.Includes}
	if o.localActionsSet || o.LocalActions != nil {
		value["local-actions"] = o.LocalActions
	}
	if o.Excludes != nil {
		value["excludes"] = o.Excludes
	}
	if o.lintNode != nil {
		value["lint"] = o.lintNode
	} else if o.Lint != nil {
		node, err := lintConfigNode(*o.Lint)
		if err != nil {
			return nil, err
		}
		value["lint"] = node
	}
	return value, nil
}

func (o Override) matches(path string) bool {
	return (filefilter.Selection{Includes: o.Includes, Excludes: o.Excludes}).Match(path)
}

func lintConfigNode(config LintConfig) (*yaml.Node, error) {
	if config.source != nil {
		return configtree.Expand(config.source, make(map[*yaml.Node]bool))
	}
	var rules yaml.Node
	if err := rules.Encode(config.Rules); err != nil {
		return nil, err
	}
	if config.Rules.Disable == nil {
		for i := 0; i < len(rules.Content); i += 2 {
			if rules.Content[i].Value == "disable" {
				rules.Content = append(rules.Content[:i], rules.Content[i+2:]...)
				break
			}
		}
	}
	value := map[string]any{}
	if config.MessageIgnores != nil {
		value["message-ignores"] = config.MessageIgnores
	}
	if config.Enabled != nil {
		value["enabled"] = config.Enabled
	}
	if len(rules.Content) != 0 {
		value["rules"] = &rules
	}
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	return &node, nil
}

// ForFile never mutates shared project settings; parallel file analyses
// each receive their own effective configuration.
func ForFile(config *Config, path, root string) (*Config, error) {
	if config == nil {
		return config, nil
	}
	if config.LocalActions != nil {
		next := *config
		var err error
		next.LocalActions, err = normalizedLocalActionSources(config.LocalActions)
		if err != nil {
			return nil, err
		}
		config = &next
	}
	if len(config.Overrides) == 0 {
		return config, nil
	}
	path = RepositoryRelativePath(path, root)
	node, err := lintConfigNode(config.Lint)
	if err != nil {
		return nil, err
	}
	if config.Lint.source == nil {
		// Project programmatically constructed Go API values through the same
		// canonical serialization before applying scoped lint overrides.
		var document yaml.Node
		if err := document.Encode(config); err != nil {
			return nil, err
		}
		if lint := field(&document, "lint"); lint != nil {
			node = lint
		}
	}
	localActions := config.LocalActions
	for _, override := range config.Overrides {
		if !override.matches(path) {
			continue
		}
		if override.localActionsSet || override.LocalActions != nil {
			if override.LocalActions == nil {
				localActions = nil
			} else {
				overlay, err := normalizedLocalActionSources(override.LocalActions)
				if err != nil {
					return nil, err
				}
				next := make(LocalActionSources)
				maps.Copy(next, localActions)
				maps.Copy(next, overlay)
				localActions = next
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
		node = configtree.Merge(expandLintShorthands(node), overlay, map[*yaml.Node]Input{})
	}
	next := *config
	next.LocalActions = localActions
	next.Lint = LintConfig{}
	content, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(content, &next.Lint); err != nil {
		return nil, err
	}
	next.Paths = nil
	next.SelfHostedRunner = SelfHostedRunnerConfig{}
	next.ConfigVariables, next.ConfigSecrets = nil, nil
	next.AssumeDefaultPermissions = DefaultPermissionsAssumptionUnset
	next.Policy = Policy{}
	projectSettings(&next)
	return &next, nil
}

// Expand scalar shorthands so a partial mapping can override individual options.
func expandLintShorthands(node *yaml.Node) *yaml.Node {
	for category := range (&LintRulesConfig{}).Groups() {
		node = expandRuleLevel(node, "rules", category)
		for _, d := range Rules() {
			if d.Group == category {
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

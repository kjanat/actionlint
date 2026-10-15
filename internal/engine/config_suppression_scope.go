package engine

import (
	"actionlint.kjanat.dev/internal/configtree"
	"go.yaml.in/yaml/v4"
)

// Resolve metadata diagnostic settings independently of workflow-only rule options.
func suppressionConfigForFile(config *Config, path, root string) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	return scopedConfigForFile(config, path, root, suppressionLintNode, false)
}

// Action metadata has no jobs or workflow policy options. Project only the
// settings consumed by action checks before merging per-file overrides.
func actionConfigForFile(config *Config, path, root string) (*Config, error) {
	selected := map[string][]string{}
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.supportsActions || descriptor.configurationScope == sourceConfiguration {
			selected[descriptor.Category] = append(selected[descriptor.Category], descriptor.Name)
		}
	}
	return scopedConfigForFile(config, path, root, func(node *yaml.Node) *yaml.Node {
		return configtree.SelectLintRules(node, selected)
	}, true)
}

func scopedConfigForFile(config *Config, path, root string, selectLint func(*yaml.Node) *yaml.Node, tools bool) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	node, err := lintConfigNode(config.Lint)
	if err != nil {
		return nil, err
	}
	next := *config
	next.Lint = LintConfig{}
	if err := selectLint(node).Decode(&next.Lint); err != nil {
		return nil, err
	}
	next.Overrides = append([]ConfigOverride(nil), config.Overrides...)
	for i := range next.Overrides {
		override := &next.Overrides[i]
		node := override.lintNode
		if node == nil && override.Lint != nil {
			node, err = lintConfigNode(*override.Lint)
			if err != nil {
				return nil, err
			}
		}
		override.lintNode = selectLint(node)
		override.Lint = nil
		if !tools {
			override.Tools, override.toolsNode = nil, nil
		}
	}
	return configForFile(&next, path, root)
}

func suppressionLintNode(node *yaml.Node) *yaml.Node {
	return configtree.SelectLintRules(node, map[string][]string{
		"correctness": {"inline-suppression", "deprecated-commands"},
		"policy":      {"disallow-suppressions"},
		"external":    {"shellcheck"},
	})
}

// Use the same configuration scope for planning and executing a selected input.
func configForDocument(config *Config, path, root string) (*Config, error) {
	if isActionInput(path) {
		return actionConfigForFile(config, path, root)
	}
	return configForFile(config, path, root)
}

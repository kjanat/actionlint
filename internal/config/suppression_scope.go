package config

import (
	configtree "actionlint.kjanat.dev/v2/internal/config/tree"

	"github.com/kjanat/go-yaml/v4"
)

// ForSuppressions resolves metadata diagnostic settings independently of workflow-only rule options.
func ForSuppressions(config *Config, path, root string) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	return scopedConfigForFile(config, path, root, suppressionLintNode, false)
}

// ForAction projects only the settings consumed by action checks before merging
// per-file overrides; action metadata has no jobs or workflow policy options.
func ForAction(config *Config, path, root string) (*Config, error) {
	selected := map[string][]string{}
	for _, descriptor := range Rules() {
		if descriptor.SupportsActions || descriptor.Scope == SourceScope {
			selected[descriptor.Group] = append(selected[descriptor.Group], descriptor.Name)
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
	next.Overrides = append([]Override(nil), config.Overrides...)
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
	return ForFile(&next, path, root)
}

func suppressionLintNode(node *yaml.Node) *yaml.Node {
	return configtree.SelectLintRules(node, map[string][]string{
		"correctness": {"inline-suppression", "deprecated-commands"},
		"policy":      {"disallow-suppressions"},
		"external":    {"shellcheck"},
	})
}

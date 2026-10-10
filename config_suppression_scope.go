package actionlint

import (
	"actionlint.kjanat.dev/internal/configtree"
	"go.yaml.in/yaml/v4"
)

// Resolve the lint settings needed to interpret metadata directives.
func suppressionConfigForFile(config *Config, path, root string) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	node, err := lintConfigNode(config.Lint)
	if err != nil {
		return nil, err
	}
	next := *config
	next.Lint = LintConfig{}
	if err := suppressionLintNode(node).Decode(&next.Lint); err != nil {
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
		override.lintNode = suppressionLintNode(node)
		override.Lint = nil
		override.Tools, override.toolsNode = nil, nil
	}
	return configForFile(&next, path, root)
}

func suppressionLintNode(node *yaml.Node) *yaml.Node {
	return configtree.SelectLintRules(node, map[string][]string{
		"correctness": {"inline-suppression"},
		"policy":      {"disallow-suppressions"},
	})
}

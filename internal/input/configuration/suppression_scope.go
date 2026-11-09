package config

import (
	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
)

// ForSuppressions resolves metadata diagnostic settings independently of workflow-only rule options.
func (ruleSet RuleSet) ForSuppressions(config *Config, path, root string) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	return ruleSet.scopedConfigForFile(config, path, root, ruleSet.suppressionLintNode, false)
}

// ForAction projects only the settings consumed by action checks before merging
// per-file overrides; action metadata has no jobs or workflow policy options.
func (ruleSet RuleSet) ForAction(config *Config, path, root string) (*Config, error) {
	selected := ruleSet.scopedRuleNames(func(descriptor Rule) bool {
		return descriptor.SupportsActions || descriptor.Scope == rulescope.SourceConfiguration
	})
	return ruleSet.scopedConfigForFile(config, path, root, func(node any) any {
		return configtree.SelectLintRules(node, selected)
	}, true)
}

func (ruleSet RuleSet) scopedConfigForFile(config *Config, path, root string, selectLint func(any) any, tools bool) (*Config, error) {
	var err error
	config, err = ruleSet.capturedRuleConfig(config)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	node, err := lintConfigValue(config.Lint)
	if err != nil {
		return nil, err
	}
	next := *config
	next.Lint = LintConfig{}
	if err := decodeValue(selectLint(node), &next.Lint); err != nil {
		return nil, err
	}
	next.Overrides = append([]Override(nil), config.Overrides...)
	for i := range next.Overrides {
		override := &next.Overrides[i]
		node := override.lintNode
		if node == nil && override.Lint != nil {
			node, err = lintConfigValue(*override.Lint)
			if err != nil {
				return nil, err
			}
		}
		override.lintNode = selectLint(node)
		override.lintSet = override.lintSet || override.Lint != nil
		override.Lint = nil
		if !tools {
			override.Tools, override.toolsNode = nil, nil
		}
	}
	return ruleSet.ForFile(&next, path, root)
}

// suppressionLintNode keeps the rules whose findings belong to the checked
// source itself, including tool providers scoped to the source.
func (ruleSet RuleSet) suppressionLintNode(node any) any {
	selected := ruleSet.scopedRuleNames(func(descriptor Rule) bool { return descriptor.Scope == rulescope.SourceConfiguration })
	return configtree.SelectLintRules(node, selected)
}

func (ruleSet RuleSet) scopedRuleNames(include func(Rule) bool) map[string][]string {
	selected := map[string][]string{}
	for _, descriptor := range ruleSet.Rules() {
		if include(descriptor) {
			names := []string{descriptor.Name}
			for _, alias := range descriptor.Aliases {
				names = append(names, alias.Name)
			}
			selected[descriptor.Group] = append(selected[descriptor.Group], names...)
			if legacy := ruleSet.LegacyGroup(descriptor.Name); legacy != "" && legacy != descriptor.Group {
				selected[legacy] = append(selected[legacy], names...)
			}
		}
	}
	return selected
}

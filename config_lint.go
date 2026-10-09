package actionlint

import (
	"fmt"

	"go.yaml.in/yaml/v4"
)

// LintConfig configures built-in analysis rules.
type LintConfig struct {
	source *yaml.Node
	// Enabled controls lint diagnostics only, leaving other features independent.
	Enabled *bool `yaml:"enabled,omitempty" jsonschema:"nullable,default=true"`
	// Rules selects diagnostic groups, individual levels and options.
	Rules LintRulesConfig `yaml:"rules" jsonschema:"nullable"`
}

// LintRulesConfig groups checks independently of their recommended defaults.
type LintRulesConfig struct {
	// Preset selects recommended, all stable, or no rules before explicit settings.
	Preset RulePreset `yaml:"preset,omitempty"`
	// Concern groups accept a level or a mapping of rule IDs to settings.
	Correctness RuleGroupConfig `yaml:"correctness,omitempty"`
	Suspicious  RuleGroupConfig `yaml:"suspicious,omitempty"`
	Security    RuleGroupConfig `yaml:"security,omitempty"`
	Policy      RuleGroupConfig `yaml:"policy,omitempty"`
	External    RuleGroupConfig `yaml:"external,omitempty"`
	// Nursery contains rules under development, excluded from stable presets.
	Nursery  RuleGroupConfig `yaml:"nursery,omitempty"`
	resolved map[string]RuleLevel
	// Disable suppresses named diagnostic rule IDs. Omission or [] disables none.
	// This applies to every diagnostic severity, including external tool warnings.
	Disable []string `yaml:"disable" jsonschema:"nullable"`
}

// UnmarshalYAML rejects misspelled lint settings.
func (c *LintConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain LintConfig
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	*c = LintConfig(next)
	c.source = n
	return nil
}

// UnmarshalYAML rejects misspelled rule groups.
func (c *LintRulesConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain LintRulesConfig
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for _, name := range next.Disable {
		if _, ok := findRuleDescriptor(name); !ok {
			return fmt.Errorf("yaml: unknown diagnostic rule %q in lint.rules.disable", name)
		}
	}
	for category, group := range LintRulesConfig(next).groups() {
		for name, setting := range group.Rules {
			d, ok := findRuleDescriptor(name)
			if !ok || d.Category != category {
				return fmt.Errorf("yaml: unknown rule %q in lint.rules.%s", name, category)
			}
			if setting.Level != "" && !setting.Level.Valid() {
				return fmt.Errorf("yaml: rule %q requires a valid level", name)
			}
			if err := setting.DecodeOptions(d.newOptions); err != nil {
				return fmt.Errorf("yaml: options for %q: %w", name, err)
			}
			group.Rules[name] = setting
		}
	}
	*c = LintRulesConfig(next)
	return nil
}

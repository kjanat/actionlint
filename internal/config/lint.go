package config

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kjanat/go-yaml/v4"
)

// LintConfig configures built-in and registered analysis rules.
type LintConfig struct {
	source *yaml.Node
	// Enabled controls lint diagnostics only, leaving other features independent.
	Enabled *bool `yaml:"enabled,omitempty" jsonschema:"nullable,default=true"`
	// Rules selects diagnostic groups, individual levels and options.
	Rules LintRulesConfig `yaml:"rules" jsonschema:"nullable"`
	// MessageIgnores maps repository-relative globs to diagnostic message regular
	// expressions. All matching paths apply; arrays replace inherited patterns.
	MessageIgnores map[string]IgnorePatterns `yaml:"message-ignores,omitempty" jsonschema:"nullable"`
}

// LintRulesConfig groups checks independently of their recommended defaults.
type LintRulesConfig struct {
	// Preset selects recommended, all stable, or no rules before explicit settings.
	Preset Preset `yaml:"preset,omitempty"`
	// Concern groups accept a level or a mapping of rule IDs to settings.
	Correctness Group `yaml:"correctness,omitempty"`
	Suspicious  Group `yaml:"suspicious,omitempty"`
	// Style reports workflows GitHub accepts that do nothing, such as jobs without steps.
	Style    Group `yaml:"style,omitempty"`
	Security Group `yaml:"security,omitempty"`
	Policy   Group `yaml:"policy,omitempty"`
	External Group `yaml:"external,omitempty"`
	// Plugins selects registered document analyzers by their unique rule names.
	Plugins Group `yaml:"plugins,omitempty"`
	// Nursery contains rules under development, excluded from stable presets.
	Nursery  Group `yaml:"nursery,omitempty"`
	resolved map[string]Level
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
	for pattern := range next.MessageIgnores {
		if !doublestar.ValidatePattern(pattern) {
			return fmt.Errorf("invalid glob pattern %q in lint.message-ignores", pattern)
		}
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
	for category, group := range (*LintRulesConfig)(&next).Groups() {
		for name, setting := range group.Rules {
			if category == "plugins" {
				if err := setting.DecodeOptions(func() any { return new(map[string]any) }); err != nil {
					return fmt.Errorf("yaml: options for %q: %w", name, err)
				}
				group.Rules[name] = setting
				continue
			}
			d, ok := FindRule(name)
			if !ok || d.Group != category {
				return fmt.Errorf("yaml: unknown rule %q in lint.rules.%s", name, category)
			}
			if setting.Level != "" && !setting.Level.Valid() {
				return fmt.Errorf("yaml: rule %q requires a valid level", name)
			}
			if err := setting.DecodeOptions(d.Options); err != nil {
				return fmt.Errorf("yaml: options for %q: %w", name, err)
			}
			group.Rules[name] = setting
		}
	}
	*c = LintRulesConfig(next)
	return nil
}

// ResolvedLevel returns the level selected for a rule after presets were applied.
func (c *LintRulesConfig) ResolvedLevel(name string) (Level, bool) {
	level, ok := c.resolved[name]
	return level, ok
}

// SetResolved records the levels selected after presets were applied.
func (c *LintRulesConfig) SetResolved(levels map[string]Level) {
	c.resolved = levels
}

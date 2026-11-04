package config

import (
	"errors"
	"fmt"
	"maps"

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
	Security    Group `yaml:"security,omitempty"`
	Conventions Group `yaml:"conventions,omitempty"`
	// Legacy groups remain readable until installed metadata permits migration.
	Style           Group `yaml:"style,omitempty"`
	Policy          Group `yaml:"policy,omitempty"`
	External        Group `yaml:"external,omitempty"`
	Plugins         Group `yaml:"plugins,omitempty"`
	Nursery         Group `yaml:"nursery,omitempty"`
	resolved        map[string]Level
	boundCategories map[string]string
	boundSettings   map[string]Setting
	// bindingSources retain settings until descriptors arrive, including
	// invalid settings hidden by later inheritance or overrides.
	bindingSources []bindingSource
	// Disable suppresses named diagnostic rule IDs. Omission or [] disables none.
	// This applies to every diagnostic severity, including external tool warnings.
	Disable []string `yaml:"disable" jsonschema:"nullable"`
}

type bindingSource struct {
	origin string
	rules  LintRulesConfig
}

func ruleBindingSources(origin string, config *Config) []bindingSource {
	sources := []bindingSource{{origin: origin, rules: config.Lint.Rules}}
	for i, override := range config.Overrides {
		if override.Lint != nil {
			sources = append(sources, bindingSource{origin: fmt.Sprintf("%s override %d", origin, i+1), rules: override.Lint.Rules})
		}
	}
	return sources
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
	if err := (*LintRulesConfig)(&next).validateSettings(); err != nil {
		return err
	}
	*c = LintRulesConfig(next)
	return nil
}

// validateSettings checks every explicit rule setting: a built-in rule must
// be in its own group, have a valid level and options of its options type;
// plugin options must decode as a mapping. Parsing calls it, and so does
// ValidateRuleNames, because settings built with NewSetting skip parsing.
func (c *LintRulesConfig) validateSettings() error {
	groups := c.Groups()
	maps.Copy(groups, c.LegacyGroups())
	for category, group := range groups {
		for name, setting := range group.Rules() {
			if name == "required-actions" {
				return errors.New("yaml: required-actions was removed; select individual action rules instead")
			}
			d, ok := FindRule(name)
			if !ok {
				// Loading does not yet have installed plugin descriptors. Their
				// names, categories and schemas bind at analysis composition.
				continue
			}
			legacyPlugin := category == "plugins" && LegacyGroup(name) == "external"
			legacyNursery := category == "nursery" && d.Nursery()
			if d.Category != category && LegacyGroup(name) != category && !legacyPlugin && !legacyNursery {
				return fmt.Errorf("yaml: rule %q belongs in lint.rules.%s, not lint.rules.%s", name, d.Category, category)
			}
			if setting.Level() != "" && !setting.Level().Valid() {
				return fmt.Errorf("yaml: rule %q requires a valid level", name)
			}
			if _, provider := toolProviders()[d.Name]; provider {
				// Installed provider replacements can supply their own schema.
				continue
			}
			if err := validateOptions(d, setting); err != nil {
				return fmt.Errorf("yaml: options for %q: %w", name, err)
			}
		}
	}
	return nil
}

// validateOptions decodes the options of setting into the rule's options type
// and discards the result. Rules without an options type accept none.
func validateOptions(d Rule, setting Setting) error {
	if !setting.HasOptions() {
		return nil
	}
	if d.Options == nil {
		return errors.New("rule does not accept options")
	}
	_, err := setting.LoadOptions(d.Options())
	return err
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

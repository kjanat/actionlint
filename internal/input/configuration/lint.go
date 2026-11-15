package configuration

import (
	"errors"
	"fmt"
	"maps"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	"github.com/bmatcuk/doublestar/v4"
)

// LintConfig configures built-in and registered analysis rules.
type LintConfig struct {
	lintState
	// Enabled controls lint diagnostics only, leaving other features independent.
	Enabled *bool `yaml:"enabled,omitempty" jsonschema:"nullable,default=true"`
	// Experimental enables nursery rules unless an explicit invocation overrides it.
	Experimental *bool `yaml:"experimental,omitempty" jsonschema:"nullable"`
	// Rules selects diagnostic groups, individual levels and options.
	Rules LintRulesConfig `yaml:"rules" jsonschema:"nullable"`
	// Lifecycle configures dated support notifications.
	Lifecycle LifecycleConfig `yaml:"lifecycle,omitempty" jsonschema:"nullable"`
	// Dependencies selects additional findings in remote dependencies that fail the check.
	Dependencies DependencyFindings `yaml:"dependencies,omitempty"`
	// MessageIgnores maps repository-relative globs to diagnostic message regular
	// expressions. All matching paths apply; arrays replace inherited patterns.
	MessageIgnores map[string]IgnorePatterns `yaml:"message-ignores,omitempty" jsonschema:"nullable"`
}

// LintRulesConfig groups checks independently of their recommended defaults.
type LintRulesConfig struct {
	ruleState
	// Preset selects recommended, all stable, or no rules before explicit settings.
	Preset Preset `yaml:"preset,omitempty"`
	// Concern groups accept a level or a mapping of rule IDs to settings.
	Correctness Group `yaml:"correctness,omitempty" clone:"shared"`
	Suspicious  Group `yaml:"suspicious,omitempty" clone:"shared"`
	Security    Group `yaml:"security,omitempty" clone:"shared"`
	Conventions Group `yaml:"conventions,omitempty" clone:"shared"`
	// Legacy groups remain readable until installed metadata permits migration.
	Style    Group `yaml:"style,omitempty" clone:"shared"`
	Policy   Group `yaml:"policy,omitempty" clone:"shared"`
	External Group `yaml:"external,omitempty" clone:"shared"`
	Plugins  Group `yaml:"plugins,omitempty" clone:"shared"`
	Nursery  Group `yaml:"nursery,omitempty" clone:"shared"`
	// Disable suppresses named diagnostic rule IDs. Omission or [] disables none.
	// This applies to every diagnostic severity, including external tool warnings.
	Disable []string `yaml:"disable" jsonschema:"nullable"`
}

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/input/configuration -type ruleState,lintState,NetworkConfig,RepositoryConfig,SuppressionsPolicy -output internal/input/configuration/owned_state.gen.go

// lintState owns private source data. Captured source is detached at decode and
// is only read through copying projections.
type lintState struct {
	source    any `clone:"shared"`
	locations map[string]Input
}

// ruleState owns prepared rule decisions. The shared handles are immutable:
// RuleSet exposes detached descriptors, Settings expose detached options, and
// captured rule sources and binding sources are only read after preparation.
type ruleState struct {
	ruleSet         RuleSet `clone:"shared"`
	resolved        map[string]Level
	resolvedFixed   map[string]bool
	boundCategories map[string]string
	boundSettings   map[string]boundSetting
	sources         *ruleSources `clone:"shared"`
	bindingSources  []bindingSource
}

// boundSetting shares an immutable Setting; its options can only be read as copies.
type boundSetting struct {
	Setting `clone:"shared"`
}

type bindingSource struct {
	origin string
	rules  LintRulesConfig
	inputs map[string]Input
	prefix []string
}

func ruleBindingSources(origin string, config *Config, locations ...map[string]Input) []bindingSource {
	var inputs map[string]Input
	if len(locations) > 0 {
		inputs = maps.Clone(locations[0])
	}
	sources := []bindingSource{{origin: origin, rules: config.Lint.Rules, inputs: inputs}}
	for i, override := range config.Overrides {
		if override.Lint != nil {
			sources = append(sources, bindingSource{origin: fmt.Sprintf("%s override %d", origin, i+1), rules: override.Lint.Rules, inputs: inputs, prefix: []string{"overrides", strconv.Itoa(i)}})
		}
	}
	return sources
}

// CopyConfig detaches public lint settings and the complete private owner state.
func (c LintConfig) CopyConfig() (any, error) {
	type plain LintConfig
	copied, err := binding.DetachPublicFields(plain(c))
	if err != nil {
		return nil, err
	}
	next := copied
	next.lintState = copyOwnedlintState(c.lintState)
	return LintConfig(next), nil
}

func (c *LintRulesConfig) CopyConfig() (any, error) {
	type plain LintRulesConfig
	copied, err := binding.DetachPublicFields(plain(*c))
	if err != nil {
		return nil, err
	}
	next := copied
	next.ruleState = copyOwnedruleState(c.ruleState)
	return LintRulesConfig(next), nil
}

// DecodeConfig rejects misspelled lint settings.
func (c *LintConfig) DecodeConfig(value any) error {
	type plain LintConfig
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	for pattern := range next.MessageIgnores {
		if !doublestar.ValidatePattern(pattern) {
			return binding.At("message-ignores", binding.At(pattern, fmt.Errorf("invalid glob pattern %q", pattern)))
		}
	}
	detached, err := binding.DetachValue(value)
	if err != nil {
		return err
	}
	*c = LintConfig(next)
	c.source = detached
	return nil
}

// DecodeConfig rejects misspelled rule groups.
func (c *LintRulesConfig) DecodeConfig(value any) error {
	type plain LintRulesConfig
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	*c = LintRulesConfig(next)
	return nil
}

// validateSettings checks every explicit rule setting: a built-in rule must
// be in its own group, have a valid level and options of its options type;
// plugin options must decode as a mapping. Rule binding calls it after structural
// decoding and also validates settings created with NewSetting.
func (ruleSet RuleSet) validateSettings(c *LintRulesConfig) error {
	groups := c.Groups()
	maps.Copy(groups, c.LegacyGroups())
	for category, group := range groups {
		for name, setting := range group.Rules() {
			if name == "required-actions" {
				return errors.New("yaml: required-actions was removed; select individual action rules instead")
			}
			d, ok := ruleSet.FindRule(name)
			if !ok {
				// Loading does not yet have installed plugin descriptors. Their
				// names, categories and schemas bind at analysis composition.
				continue
			}
			legacyPlugin := category == "plugins" && (ruleSet.registered[ruleSet.ResolveAlias(name)] || ruleSet.LegacyGroup(name) == "external")
			legacyNursery := category == "nursery" && d.Experimental()
			if d.Category != category && ruleSet.LegacyGroup(name) != category && !legacyPlugin && !legacyNursery {
				return fmt.Errorf("yaml: rule %q belongs in lint.rules.%s, not lint.rules.%s", name, d.Category, category)
			}
			if setting.Level() != "" && !setting.Level().Valid() {
				return fmt.Errorf("yaml: invalid rule level %q for %q", setting.Level(), name)
			}
			if _, overridden := ruleSet.optionOverrides[d.Name]; overridden {
				continue
			}
			if _, provider := toolProviders()[d.Name]; provider {
				// Installed provider replacements can supply their own schema.
				continue
			}
			if err := validateOptions(d, setting); err != nil {
				return ruleOptionsError(category, name, err)
			}
		}
	}
	return nil
}

func ruleOptionsError(category, name string, err error) error {
	return binding.Context(fmt.Sprintf("options for rule %q", name), binding.At(category, binding.At(name, binding.At("options", err))))
}

// validateOptions checks a rule's JSON Schema or decodes its registered Go type.
// A rule with neither contract accepts no options.
func validateOptions(d Rule, setting Setting) error {
	if !setting.HasOptions() {
		return nil
	}
	if len(d.OptionsSchema) > 0 {
		return validateRegisteredOptions(d, setting)
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

// CopyConfig copies the owned configuration state.
func (v NetworkConfig) CopyConfig() (any, error) { return copyOwnedNetworkConfig(v), nil }

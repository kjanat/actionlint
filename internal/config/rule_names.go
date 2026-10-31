package config

import (
	"fmt"
	"maps"
	"slices"
)

// ValidateRuleNames binds structurally decoded suppression settings to the
// rules registered by this invocation. Additional names never change global state.
//
// It also checks the rule settings in config.Lint the way parsing does, so a
// setting built with NewSetting whose options do not decode into the rule's
// options type is an error here instead of being ignored later. Settings in
// config.Overrides are checked when ForFile merges them for a matching file.
func ValidateRuleNames(config *Config, additional ...string) error {
	if config == nil {
		return nil
	}
	if err := config.Lint.Rules.validateSettings(); err != nil {
		return err
	}
	for _, ignore := range config.Ignores {
		if err := ignore.Validate(additional...); err != nil {
			return err
		}
	}
	validate := func(policy *SuppressionsPolicy) error {
		for _, name := range policy.Rules() {
			if !Suppressible(name, additional...) {
				if node := policy.locations[name]; node != nil {
					return suppressionConfigError(node, fmt.Sprintf("unknown suppressible rule %q", name))
				}
				return fmt.Errorf("unknown suppressible rule %q", name)
			}
		}
		return nil
	}
	if err := validate(config.Policy.DisallowSuppressions); err != nil {
		return err
	}
	// Loading knows only the combined rule names, so a tool entry must at least
	// name a registered rule. The composition layer, which knows which
	// registrations are tool providers, binds entries with ValidateToolNames.
	if err := ValidateToolNames(config, additional...); err != nil {
		return err
	}
	lints := []*LintConfig{&config.Lint}
	for _, override := range config.Overrides {
		if override.Lint != nil {
			lints = append(lints, override.Lint)
		}
	}
	for _, lint := range lints {
		for _, name := range lint.Rules.Disable {
			if !Suppressible(name, additional...) {
				return fmt.Errorf("yaml: unknown diagnostic rule %q in lint.rules.disable", name)
			}
		}
		for name := range lint.Rules.Plugins.Rules() {
			if !slices.Contains(additional, name) {
				return fmt.Errorf("yaml: unknown registered rule %q in lint.rules.plugins", name)
			}
		}
		// Parsing only accepts disallow-suppressions in its own group, so that is
		// the one setting whose options name rules.
		suppressions, _ := FindRule("disallow-suppressions")
		setting, _ := lint.Rules.Groups()[suppressions.Group].Rule(suppressions.Name)
		if policy := new(SuppressionsPolicy); loadOptions(setting, policy) {
			if err := validate(policy); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateToolNames binds every tools.<name> entry, including those of file
// overrides, to a bundled tool provider or to one of providers, the rule
// names of the tool providers registered by this invocation.
func ValidateToolNames(config *Config, providers ...string) error {
	if config == nil {
		return nil
	}
	tools := []ToolsConfig{config.Tools}
	for _, override := range config.Overrides {
		if override.Tools != nil {
			tools = append(tools, *override.Tools)
		}
	}
	for _, entries := range tools {
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			if _, bundled := toolProviders()[name]; !bundled && !slices.Contains(providers, name) {
				return fmt.Errorf("yaml: unknown tool %q in tools; it names no bundled or registered tool provider", name)
			}
		}
	}
	return nil
}

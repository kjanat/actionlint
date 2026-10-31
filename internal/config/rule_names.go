package config

import (
	"fmt"
	"slices"
)

// ValidateRuleNames binds structurally decoded suppression settings to the
// rules registered by this invocation. Additional names never change global state.
func ValidateRuleNames(config *Config, additional ...string) error {
	if config == nil {
		return nil
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

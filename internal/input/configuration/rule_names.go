package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

// ValidateRuleNames binds structurally decoded suppression settings to the
// rules registered by this invocation. Additional names never change global state.
//
// It also checks the rule settings in config.Lint the way parsing does, so a
// setting built with NewSetting produces an error if its options cannot be
// decoded into the rule's registered options type. Settings in
// config.Overrides receive complete descriptor validation in ResolveRules.
func (ruleSet RuleSet) ValidateRuleNames(config *Config, additional ...string) error {
	if config == nil {
		return nil
	}
	config.Lint.Rules.ruleSet = ruleSet

	if err := ruleSet.validateSettings(&config.Lint.Rules); err != nil {
		return binding.At("lint", binding.At("rules", err))
	}
	for _, ignore := range config.Ignores {
		if err := ruleSet.ValidateIgnore(ignore, additional...); err != nil {
			return err
		}
	}
	validate := func(policy *SuppressionsPolicy) error {
		if policy != nil {
			policy.names = ruleSet.Names()
		}
		for _, name := range policy.Rules() {
			if !ruleSet.Suppressible(name, additional...) {
				if index, found := policy.locations[name]; found {
					return binding.At("rules", binding.At(strconv.Itoa(index), fmt.Errorf("unknown suppressible rule %q", name)))
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
	providers := slices.Concat(additional, slices.Sorted(maps.Keys(ruleSet.providers)))
	if err := ValidateToolNames(config, providers...); err != nil {
		return err
	}
	lints := []*LintConfig{&config.Lint}
	for _, override := range config.Overrides {
		if override.Lint != nil {
			lints = append(lints, override.Lint)
		}
	}
	for _, lint := range lints {
		lint.Rules.ruleSet = ruleSet
		for i, selector := range lint.Dependencies.FailOnRules {
			name, code, qualified := strings.Cut(selector, "/")
			if !ruleSet.Suppressible(name, additional...) || qualified && (code == "" || strings.ContainsAny(code, "/ \t\r\n")) {
				return binding.At("lint", binding.At("dependencies", binding.At("fail-on-rules", binding.At(strconv.Itoa(i), fmt.Errorf("invalid rule selector %q", selector)))))
			}
		}
		for _, name := range lint.Rules.Disable {
			if !ruleSet.Suppressible(name, additional...) {
				return fmt.Errorf("yaml: unknown diagnostic rule %q in lint.rules.disable", name)
			}
		}
		// Suppression policy options also refer to registered rule names.
		suppressions, _ := ruleSet.FindRule("forbidden-suppression")
		setting, found := ruleSet.canonicalSetting(lint.Rules.Groups()[suppressions.Category], suppressions.Name)
		if !found {
			setting, _ = ruleSet.canonicalSetting(lint.Rules.Policy, suppressions.Name)
		}
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

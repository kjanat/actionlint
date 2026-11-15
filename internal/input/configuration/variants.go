package configuration

import (
	"path"
	"slices"
	"strings"
)

// ConfigCandidates enumerates tool-selection states across configuration overrides.
func ConfigCandidates(name string, config *Config) ([]*Config, error) {
	return ConfigVariants(config, func(config *Config) (toolGateState, error) {
		return toolGateConfig(name, config), nil
	})
}

// ConfigVariants applies possible override combinations and keeps one configuration
// per distinct key. The caller chooses which settings the key includes.
func ConfigVariants[K comparable](config *Config, key func(*Config) (K, error)) ([]*Config, error) {
	if config == nil {
		return []*Config{nil}, nil
	}
	// Keep baseline and possible overlay configurations without discovering files.
	// Apply catch-all overrides to every candidate in order.
	candidates := []*Config{config}
	for _, override := range config.Overrides {
		universal := len(override.Excludes) == 0 && slices.ContainsFunc(override.Includes, func(pattern string) bool {
			pattern = path.Clean(pattern)
			return pattern == "**" || pattern == "**/*"
		}) &&
			!slices.ContainsFunc(override.Includes, func(pattern string) bool { return strings.HasPrefix(pattern, "!") })
		override.Includes, override.Excludes = []string{"**"}, nil
		start := 0
		if !universal {
			start = len(candidates)
			candidates = append(candidates, candidates...)
		}
		for i := start; i < len(candidates); i++ {
			candidate := *candidates[i]
			candidate.Overrides = []Override{override}
			resolved, err := candidate.RuleSet().ForFile(&candidate, "action.yml", "")
			if err != nil {
				return nil, err
			}
			candidates[i] = resolved
		}
		seen := make(map[K]bool, len(candidates))
		unique := candidates[:0]
		for _, candidate := range candidates {
			k, err := key(candidate)
			if err != nil {
				return nil, err
			}
			if !seen[k] {
				seen[k] = true
				unique = append(unique, candidate)
			}
		}
		candidates = unique
	}
	return candidates, nil
}

type backendKind uint8

const (
	backendDefault backendKind = iota
	backendProcess
	backendWasm
)

type toolGateState struct {
	tool, lint, disabled, resolved bool
	toolSet, lintSet               bool
	backend                        backendKind
	preset                         Preset
	current, legacy                ruleGroupGate
	resolution                     Level
}

type ruleGroupGate struct {
	preset      Preset
	group, rule Level
	explicit    bool
}

func groupGate(group Group, name string) ruleGroupGate {
	setting, explicit := group.Rule(name)
	return ruleGroupGate{preset: group.Preset(), group: group.Level(), rule: setting.Level(), explicit: explicit}
}

func toolGateConfig(name string, config *Config) toolGateState {
	enabled, backend := config.Tools[name].Enabled, config.Tools[name].Backend
	kind := backendDefault
	if _, ok := backend.Process(); ok {
		kind = backendProcess
	} else if _, ok := backend.Wasm(); ok {
		kind = backendWasm
	}
	rules := config.Lint.Rules
	ruleSet := config.RuleSet()
	descriptor, _ := ruleSet.FindRule(name)
	legacyCategory := ruleSet.LegacyGroup(name)
	legacy, found := rules.LegacyGroups()[legacyCategory]
	if !found {
		legacy = rules.Groups()[legacyCategory]
	}
	resolution, resolved := rules.ResolvedLevel(name)
	return toolGateState{
		tool:    enabled == nil || *enabled,
		lint:    config.Lint.Enabled == nil || *config.Lint.Enabled,
		toolSet: enabled != nil, lintSet: config.Lint.Enabled != nil,
		backend:  kind,
		disabled: slices.Contains(rules.Disable, name), resolved: resolved,
		preset:  rules.Preset,
		current: groupGate(rules.Groups()[descriptor.Category], name), legacy: groupGate(legacy, name), resolution: resolution,
	}
}

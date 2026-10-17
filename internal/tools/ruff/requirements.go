package ruff

import (
	"path"
	"slices"
	"strings"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func projectMayEnable(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	if config != nil && config.Lint.Enabled != nil && !*config.Lint.Enabled {
		return false, nil
	}
	possible, err := mayEnable(config)
	if possible || err != nil || !project || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	// A workflow-only override does not apply to a referenced action.yml. Keep
	// the baseline available without inventing a second composite graph here.
	return mayEnable(projectConfig)
}

// mayEnable reports whether any applicable configuration could enable Ruff.
// A composite's path can match different overrides than its calling workflow.
// Tool planning intentionally avoids reading workflows, so preserve a checker
// when an override could enable it. Disabling-only overrides do not turn it on.
func mayEnable(config *repoconfig.Config) (bool, error) {
	if config == nil {
		return true, nil
	}
	// Keep baseline and possible overlay configurations without discovering files.
	// Apply catch-all overrides to every candidate in order.
	candidates := []*repoconfig.Config{config}
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
			candidate.Overrides = []repoconfig.Override{override}
			resolved, err := repoconfig.ForFile(&candidate, "action.yml", "")
			if err != nil {
				return false, err
			}
			candidates[i] = resolved
		}
		// Retain precedence-bearing gate states while collapsing unrelated options.
		seen := make(map[ruffGateState]bool, len(candidates))
		unique := candidates[:0]
		for _, candidate := range candidates {
			key := ruffGateConfig(candidate)
			if !seen[key] {
				seen[key] = true
				unique = append(unique, candidate)
			}
		}
		candidates = unique
	}
	for _, candidate := range candidates {
		if repoconfig.DiagnosticLevel(candidate, "ruff") != "off" && (candidate.Tools.Ruff.Enabled == nil || *candidate.Tools.Ruff.Enabled) {
			return true, nil
		}
	}
	return false, nil
}

type ruffGateState struct {
	tool, lint, disabled, explicit, resolved bool
	toolSet, lintSet                         bool
	preset, groupPreset                      repoconfig.Preset
	group, rule, resolution                  repoconfig.Level
}

func ruffGateConfig(config *repoconfig.Config) ruffGateState {
	rules := config.Lint.Rules
	rule, explicit := rules.External.Rules["ruff"]
	resolution, resolved := rules.ResolvedLevel("ruff")
	return ruffGateState{
		tool:    config.Tools.Ruff.Enabled == nil || *config.Tools.Ruff.Enabled,
		lint:    config.Lint.Enabled == nil || *config.Lint.Enabled,
		toolSet: config.Tools.Ruff.Enabled != nil, lintSet: config.Lint.Enabled != nil,
		disabled: slices.Contains(rules.Disable, "ruff"), explicit: explicit, resolved: resolved,
		preset: rules.Preset, groupPreset: rules.External.Preset,
		group: rules.External.Level, rule: rule.Level, resolution: resolution,
	}
}

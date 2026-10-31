package selection

import (
	"path"
	"slices"
	"strings"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
)

// ProjectMayEnable preserves a tool when a calling workflow or composite can enable it.
func ProjectMayEnable(name string, config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return projectMayMatch(config, projectConfig, project, func(config *repoconfig.Config) (bool, error) { return mayEnable(name, config) })
}

// ProjectMayNeedProcess checks possible composite overrides without parsing workflow inputs.
func ProjectMayNeedProcess(name string, config, projectConfig *repoconfig.Config, project bool, command string, explicit *processconfig.Options) (bool, error) {
	return ProjectMayNeedProcessFor(name, config, projectConfig, project, func(candidate *repoconfig.Config) bool {
		return processconfig.Enabled(command, explicit) && Module(name, command, explicit, candidate) == nil
	})
}

// ProjectMayNeedProcessFor resolves a host's process choice for each enabled candidate.
func ProjectMayNeedProcessFor(name string, config, projectConfig *repoconfig.Config, project bool, needsProcess func(*repoconfig.Config) bool) (bool, error) {
	return projectMayMatch(config, projectConfig, project, func(config *repoconfig.Config) (bool, error) {
		candidates, err := ConfigCandidates(name, config)
		if err != nil {
			return false, err
		}
		for _, candidate := range candidates {
			if configEnabled(name, candidate) && needsProcess(candidate) {
				return true, nil
			}
		}
		return false, nil
	})
}

func projectMayMatch(config, projectConfig *repoconfig.Config, project bool, match func(*repoconfig.Config) (bool, error)) (bool, error) {
	if config != nil && config.Lint.Enabled != nil && !*config.Lint.Enabled {
		return false, nil
	}
	possible, err := match(config)
	if possible || err != nil || !project || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	// A workflow-only override does not apply to a referenced action.yml. Keep
	// the baseline available without inventing a second composite graph here.
	return match(projectConfig)
}

// mayEnable reports whether any applicable configuration could enable the tool.
// A composite's path can match different overrides than its calling workflow.
// Tool planning intentionally avoids reading workflows, so preserve a checker
// when an override could enable it. Disabling-only overrides do not turn it on.
func mayEnable(name string, config *repoconfig.Config) (bool, error) {
	candidates, err := ConfigCandidates(name, config)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(candidates, func(config *repoconfig.Config) bool { return configEnabled(name, config) }), nil
}

func configEnabled(name string, config *repoconfig.Config) bool {
	if config == nil {
		return true
	}
	enabled := config.Tools.Shellcheck.Enabled
	if name == "ruff" {
		enabled = config.Tools.Ruff.Enabled
	}
	return repoconfig.DiagnosticLevel(config, name) != "off" && (enabled == nil || *enabled)
}

// ConfigCandidates enumerates tool-selection states across configuration overrides.
func ConfigCandidates(name string, config *repoconfig.Config) ([]*repoconfig.Config, error) {
	return ConfigVariants(config, func(config *repoconfig.Config) (toolGateState, error) {
		return toolGateConfig(name, config), nil
	})
}

// ConfigVariants enumerates the configurations a referenced action.yml can
// resolve to under config's overrides, keeping one variant per distinct key.
// The key decides which differences matter to the caller: tool selection
// compares gate state, a document analyzer compares what it negotiates.
func ConfigVariants[K comparable](config *repoconfig.Config, key func(*repoconfig.Config) (K, error)) ([]*repoconfig.Config, error) {
	if config == nil {
		return []*repoconfig.Config{nil}, nil
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
	tool, lint, disabled, explicit, resolved bool
	toolSet, lintSet                         bool
	backend                                  backendKind
	preset, groupPreset                      repoconfig.Preset
	group, rule, resolution                  repoconfig.Level
}

func toolGateConfig(name string, config *repoconfig.Config) toolGateState {
	enabled, backend := config.Tools.Shellcheck.Enabled, config.Tools.Shellcheck.Backend
	if name == "ruff" {
		enabled, backend = config.Tools.Ruff.Enabled, config.Tools.Ruff.Backend
	}
	kind := backendDefault
	if backend != nil {
		if backend.Process != nil {
			kind = backendProcess
		} else if backend.Wasm != nil {
			kind = backendWasm
		}
	}
	rules := config.Lint.Rules
	rule, explicit := rules.External.Rule(name)
	resolution, resolved := rules.ResolvedLevel(name)
	return toolGateState{
		tool:    enabled == nil || *enabled,
		lint:    config.Lint.Enabled == nil || *config.Lint.Enabled,
		toolSet: enabled != nil, lintSet: config.Lint.Enabled != nil,
		backend:  kind,
		disabled: slices.Contains(rules.Disable, name), explicit: explicit, resolved: resolved,
		preset: rules.Preset, groupPreset: rules.External.Preset(),
		group: rules.External.Level(), rule: rule.Level(), resolution: resolution,
	}
}

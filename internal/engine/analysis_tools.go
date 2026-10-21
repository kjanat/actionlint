package engine

import (
	"actionlint.kjanat.dev/internal/repoconfig"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/fsread"
)

// ExternalToolRequirements identifies tools enabled by the selected configurations.
type ExternalToolRequirements struct {
	Shellcheck bool `json:"shellcheck"`
	// Deprecated: Pyflakes integration was removed. This field is always false.
	Pyflakes bool `json:"pyflakes"`
	Ruff     bool `json:"ruff"`
}

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows and actions.
// Relative paths resolve against the session's WorkingDir; absolute paths are unchanged.
func (a *AnalysisSession) RequiredTools(paths []string) (ExternalToolRequirements, error) {
	var needed ExternalToolRequirements
	if len(paths) == 0 {
		paths = []string{a.cwd}
		project, err := a.projects.At(a.cwd)
		if err != nil {
			return needed, err
		}
		if project != nil {
			paths, err = repositoryPaths(a.ctx, project, fsread.New(a.ctx, nil, nil))
			if err != nil {
				return needed, err
			}
		}
	}
	for _, path := range paths {
		if err := a.ctx.Err(); err != nil {
			return needed, err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.cwd, path)
		}
		project, err := a.projects.At(path)
		if err != nil {
			return needed, err
		}
		config, err := a.configForProject(project)
		if err != nil {
			return needed, err
		}
		root := a.cwd
		if project != nil {
			root = project.RootDir()
		}
		if !config.IncludesFile(path, root) {
			continue
		}
		projectConfig := config
		config, err = configForDocument(config, path, root)
		if err != nil {
			return needed, err
		}
		config = applyRulePresets(a.request.RulePresets, config)
		c := ruleContext{
			config: config, shellcheck: a.request.ShellCheck,
			shellcheckOptions: a.request.ShellcheckOptions,
			ruff:              a.request.Ruff, ruffOptions: a.request.RuffOptions,
		}
		for _, rule := range builtinRuleDescriptors() {
			if rule.Name == "ruff" {
				possible, err := ruffProjectConfigMayNeedProcess(config, projectConfig, project != nil, c.ruff, c.ruffOptions)
				if err != nil {
					return needed, err
				}
				needed.Ruff = needed.Ruff || (externalCommandEnabled(c.ruff, c.ruffOptions) && possible)
				continue
			}
			if diagnosticLevel(config, rule.Name) == "off" {
				continue
			}
			if rule.Name == "shellcheck" {
				needed.Shellcheck = needed.Shellcheck || rule.enabled(c) && configuredScriptModule("shellcheck", c.shellcheck, c.shellcheckOptions, config) == nil
			}
		}
	}
	return needed, nil
}

func ruffProjectConfigMayEnable(config, projectConfig *Config, project bool) (bool, error) {
	if config != nil && config.Lint.Enabled != nil && !*config.Lint.Enabled {
		return false, nil
	}
	possible, err := ruffConfigMayEnable(config)
	if possible || err != nil || !project || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	return ruffConfigMayEnable(projectConfig)
}

func ruffProjectConfigMayNeedProcess(config, projectConfig *Config, project bool, requested string, explicit *ExternalCommandOptions) (bool, error) {
	if config != nil && config.Lint.Enabled != nil && !*config.Lint.Enabled {
		return false, nil
	}
	possible, err := ruffConfigMayNeedProcess(config, requested, explicit)
	if possible || err != nil || !project || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	// A workflow-only override does not apply to a referenced action.yml. Keep
	// the baseline available without inventing a second composite graph here.
	return ruffConfigMayNeedProcess(projectConfig, requested, explicit)
}

func ruffConfigMayNeedProcess(config *Config, requested string, explicit *ExternalCommandOptions) (bool, error) {
	candidates, err := ruffConfigCandidates(config)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		if ruffConfigEnabled(candidate) && configuredScriptModule("ruff", requested, explicit, candidate) == nil {
			return true, nil
		}
	}
	return false, nil
}

// A composite's path can match different overrides than its calling workflow.
// Tool planning intentionally avoids reading workflows, so preserve a checker
// when an override could enable it. Disabling-only overrides do not turn it on.
func ruffConfigMayEnable(config *Config) (bool, error) {
	candidates, err := ruffConfigCandidates(config)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(candidates, ruffConfigEnabled), nil
}

func ruffConfigEnabled(config *Config) bool {
	return config == nil || diagnosticLevel(config, "ruff") != "off" && (config.Tools.Ruff.Enabled == nil || *config.Tools.Ruff.Enabled)
}

func ruffConfigCandidates(config *Config) ([]*Config, error) {
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
			candidate.Overrides = []ConfigOverride{override}
			resolved, err := repoconfig.ForFile(&candidate, "action.yml", "")
			if err != nil {
				return nil, err
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
	return candidates, nil
}

type ruffGateState struct {
	tool, lint, disabled, explicit, resolved bool
	toolSet, lintSet                         bool
	wasm                                     bool
	preset, groupPreset                      RulePreset
	group, rule, resolution                  RuleLevel
}

func ruffGateConfig(config *Config) ruffGateState {
	rules := config.Lint.Rules
	rule, explicit := rules.External.Rules["ruff"]
	resolution, resolved := rules.ResolvedLevel("ruff")
	return ruffGateState{
		tool:    config.Tools.Ruff.Enabled == nil || *config.Tools.Ruff.Enabled,
		lint:    config.Lint.Enabled == nil || *config.Lint.Enabled,
		toolSet: config.Tools.Ruff.Enabled != nil, lintSet: config.Lint.Enabled != nil,
		wasm:     config.Tools.Ruff.Backend != nil && config.Tools.Ruff.Backend.Wasm != nil,
		disabled: slices.Contains(rules.Disable, "ruff"), explicit: explicit, resolved: resolved,
		preset: rules.Preset, groupPreset: rules.External.Preset,
		group: rules.External.Level, rule: rule.Level, resolution: resolution,
	}
}

package actionlint

import (
	"path/filepath"
	"slices"
	"strings"
)

// ExternalToolRequirements identifies tools enabled by the selected configurations.
type ExternalToolRequirements struct {
	Shellcheck bool `json:"shellcheck"`
	// Deprecated: Pyflakes integration was removed. This field is always false.
	Pyflakes bool `json:"pyflakes"`
	Ruff     bool `json:"ruff"`
}

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows.
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
			paths, err = workflowPaths(project.WorkflowsDir())
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
		if !config.includesFile(path, root) {
			continue
		}
		projectConfig := config
		config, err = configForFile(config, path, root)
		if err != nil {
			return needed, err
		}
		config = a.request.RulePresets.apply(config)
		c := ruleContext{
			config: config, shellcheck: a.request.ShellCheck,
			shellcheckOptions: a.request.ShellcheckOptions,
			ruff:              a.request.Ruff, ruffOptions: a.request.RuffOptions,
		}
		for _, rule := range builtinRuleDescriptors() {
			if rule.Name == "ruff" {
				possible, err := ruffProjectConfigMayEnable(config, projectConfig, project != nil)
				if err != nil {
					return needed, err
				}
				needed.Ruff = needed.Ruff || (externalCommandEnabled(c.ruff, c.ruffOptions) && possible)
				continue
			}
			if config.diagnosticLevel(rule.Name) == "off" {
				continue
			}
			if rule.Name == "shellcheck" {
				needed.Shellcheck = needed.Shellcheck || rule.enabled(c)
			}
		}
	}
	return needed, nil
}

func ruffProjectConfigMayEnable(config, projectConfig *Config, project bool) (bool, error) {
	possible, err := ruffConfigMayEnable(config)
	if possible || err != nil || !project || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	// A workflow-only override does not apply to a referenced action.yml. Keep
	// the baseline available without inventing a second composite graph here.
	return ruffConfigMayEnable(projectConfig)
}

// A composite's path can match different overrides than its calling workflow.
// Tool planning intentionally avoids reading workflows, so preserve a checker
// when an override could enable it. Disabling-only overrides do not turn it on.
func ruffConfigMayEnable(config *Config) (bool, error) {
	if config == nil {
		return true, nil
	}
	// Keep a baseline plus each potentially matching overlay. A catch-all
	// applies to every candidate in order, including earlier re-enabling rules.
	// Other globs remain conservative; this is not a separate file discovery pass.
	candidates := []*Config{config}
	for _, override := range config.Overrides {
		universal := len(override.Excludes) == 0 && slices.Contains(override.Includes, "**") &&
			!slices.ContainsFunc(override.Includes, func(pattern string) bool { return strings.HasPrefix(pattern, "!") })
		override.Includes, override.Excludes = []string{"**"}, nil
		if !universal {
			candidates = append(candidates, candidates[0])
		}
		start := 0
		if !universal {
			start = len(candidates) - 1
		}
		for i := start; i < len(candidates); i++ {
			candidate := *candidates[i]
			candidate.Overrides = []ConfigOverride{override}
			resolved, err := configForFile(&candidate, "action.yml", "")
			if err != nil {
				return false, err
			}
			candidates[i] = resolved
		}
	}
	ruleEnabled, toolEnabled := false, false
	for _, candidate := range candidates {
		ruleEnabled = ruleEnabled || candidate.diagnosticLevel("ruff") != "off"
		toolEnabled = toolEnabled || candidate.Tools.Ruff.Enabled == nil || *candidate.Tools.Ruff.Enabled
	}
	return ruleEnabled && toolEnabled, nil
}

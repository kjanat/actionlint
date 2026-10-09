package actionlint

import "path/filepath"

// ExternalToolRequirements identifies tools enabled by the selected configurations.
type ExternalToolRequirements struct {
	Shellcheck bool `json:"shellcheck"`
	// Deprecated: Pyflakes integration was removed. This field is always false.
	Pyflakes bool `json:"pyflakes"`
}

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows.
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
		config, err = configForFile(config, path, root)
		if err != nil {
			return needed, err
		}
		config = a.request.RulePresets.apply(config)
		if config.diagnosticLevel("shellcheck") == "off" {
			continue
		}
		c := ruleContext{
			config: config, shellcheck: a.request.ShellCheck,
			shellcheckOptions: a.request.ShellcheckOptions,
		}
		for _, rule := range builtinRuleDescriptors() {
			if rule.Name == "shellcheck" {
				needed.Shellcheck = needed.Shellcheck || rule.enabled(c)
			}
		}
	}
	return needed, nil
}

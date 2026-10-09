package actionlint

import "path/filepath"

// ExternalToolRequirements identifies tools enabled by the selected configurations.
type ExternalToolRequirements struct {
	Shellcheck bool `json:"shellcheck"`
}

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths selects the working directory's project.
func (a *AnalysisSession) RequiredTools(paths []string) (ExternalToolRequirements, error) {
	var needed ExternalToolRequirements
	if len(paths) == 0 {
		paths = []string{a.cwd}
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

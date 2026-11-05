package workspace

import (
	"context"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	"actionlint.kjanat.dev/v2/internal/input/files"
)

// ToolOptions asks providers which tools are possible for a resolved configuration.
type ToolOptions struct {
	Presets       repoconfig.Presets
	Registrations []repoconfig.RuleRegistration
	Required      func(*repoconfig.Config, *repoconfig.Config) ([]string, error)
}
type ExternalToolRequirements map[string]bool

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows and actions.
// Relative paths resolve against the session's WorkingDir; absolute paths are unchanged.
func (a *Session) RequiredTools(ctx context.Context, paths []string, options ToolOptions) (ExternalToolRequirements, error) {
	needed := ExternalToolRequirements{}
	rules, err := a.rules.WithRegisteredRules(options.Registrations...)
	if err != nil {
		return nil, err
	}
	explicit := make(map[string]bool)
	if len(paths) == 0 {
		paths = []string{a.WorkingDir()}
		project, err := a.ProjectAt(a.WorkingDir())
		if err != nil {
			return needed, err
		}
		if project != nil {
			paths, err = discovery.Documents(ctx, project.RootDir(), files.New(ctx, nil, nil), discovery.Discovery{NoIgnore: a.noIgnore})
			if err != nil {
				return needed, err
			}
		}
	} else {
		resolved := make([]string, len(paths))
		for index, path := range paths {
			var err error
			path, err = ExpandHome(path)
			if err != nil {
				return needed, err
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(a.WorkingDir(), path)
			}
			resolved[index] = path
		}
		selection, err := FindFiles(ctx, resolved, a.noIgnore)
		if err != nil {
			return needed, err
		}
		paths = nil
		for _, file := range selection.Files {
			paths = append(paths, file.Path)
			explicit[absPath(file.Path)] = file.Explicit
		}
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return needed, err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.WorkingDir(), path)
		}
		project, err := a.ProjectAt(path)
		if err != nil {
			return needed, err
		}
		config, err := a.ConfigForProject(project)
		if err != nil {
			return needed, err
		}
		root := a.WorkingDir()
		if project != nil {
			root = project.RootDir()
		}
		if !config.IncludesSelectedFile(path, root, explicit[path]) {
			continue
		}
		projectConfig := config
		config, err = rules.ForDocument(config, path, root)
		if err != nil {
			return needed, err
		}
		config, err = a.rules.ResolveRegisteredRules(options.Presets, config, options.Registrations...)
		if err != nil {
			return needed, err
		}
		repositoryRoot := ""
		if project != nil {
			repositoryRoot = project.RootDir()
		}
		names, err := options.Required(config, repoconfig.InvocationPlanningConfig(config, projectConfig, repositoryRoot))
		if err != nil {
			return needed, err
		}
		for _, name := range names {
			needed[name] = true
		}
	}
	return needed, nil
}

package workspace

import (
	"context"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// ToolOptions asks providers which tools are possible for a resolved configuration.
type ToolOptions struct {
	Presets       repoconfig.Presets
	Registrations []repoconfig.RuleRegistration
	Required      func(*repoconfig.Config, *repoconfig.Config, bool) ([]string, error)
}
type ExternalToolRequirements map[string]bool

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows and actions.
// Relative paths resolve against the session's WorkingDir; absolute paths are unchanged.
func (a *Session) RequiredTools(ctx context.Context, paths []string, options ToolOptions) (ExternalToolRequirements, error) {
	needed := ExternalToolRequirements{}
	explicit := make(map[string]bool)
	if len(paths) == 0 {
		paths = []string{a.WorkingDir()}
		project, err := a.ProjectAt(a.WorkingDir())
		if err != nil {
			return needed, err
		}
		if project != nil {
			paths, err = projectpath.Documents(ctx, project.RootDir(), fsread.New(ctx, nil, nil), projectpath.Discovery{NoIgnore: a.noIgnore})
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
		config, err = repoconfig.ForDocument(config, path, root)
		if err != nil {
			return needed, err
		}
		config, err = repoconfig.ResolveRegisteredRules(options.Presets, config, options.Registrations...)
		if err != nil {
			return needed, err
		}
		names, err := options.Required(config, projectConfig, project != nil)
		if err != nil {
			return needed, err
		}
		for _, name := range names {
			needed[name] = true
		}
	}
	return needed, nil
}

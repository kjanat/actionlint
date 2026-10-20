package workspace

import (
	"context"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
)

// ToolOptions asks providers which tools are possible for a resolved configuration.
type ToolOptions struct {
	Presets  repoconfig.Presets
	Required func(*repoconfig.Config, *repoconfig.Config, bool) ([]string, error)
}
type ExternalToolRequirements map[string]bool

// RequiredTools resolves configuration for each input without reading workflows or
// starting analyzers. Empty paths discovers the working directory's workflows and actions.
// Relative paths resolve against the session's WorkingDir; absolute paths are unchanged.
func (a *Session) RequiredTools(ctx context.Context, paths []string, options ToolOptions) (ExternalToolRequirements, error) {
	needed := ExternalToolRequirements{}
	if len(paths) == 0 {
		paths = []string{a.WorkingDir()}
		project, err := a.ProjectAt(a.WorkingDir())
		if err != nil {
			return needed, err
		}
		if project != nil {
			paths, err = RepositoryPaths(ctx, project, fsread.New(ctx, nil, nil))
			if err != nil {
				return needed, err
			}
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
		if !config.IncludesFile(path, root) {
			continue
		}
		projectConfig := config
		config, err = repoconfig.ForDocument(config, path, root)
		if err != nil {
			return needed, err
		}
		config = repoconfig.ResolveRules(options.Presets, config)
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

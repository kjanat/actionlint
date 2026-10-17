package application

import (
	"context"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

type ToolRequirements = workspace.ExternalToolRequirements

// RequiredTools resolves configuration without parsing sources or loading runtimes.
func RequiredTools(ctx context.Context, options Options, paths []string) (ToolRequirements, error) {
	if _, err := repoconfig.CompileIgnorePatterns(options.IgnorePatterns); err != nil {
		return ToolRequirements{}, err
	}
	session, err := workspace.New(workspace.Options{Context: ctx, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, ReadFile: options.ReadFile})
	if err != nil {
		return ToolRequirements{}, err
	}
	return session.RequiredTools(ctx, paths, workspace.ToolOptions{Presets: options.Presets, Required: func(cfg, project *repoconfig.Config, inside bool) ([]string, error) {
		var names []string
		for _, r := range options.Tools {
			if r.NeedsProcess == nil {
				continue
			}
			needed, err := r.NeedsProcess(cfg, project, inside)
			if err != nil {
				return nil, err
			}
			if needed {
				names = append(names, r.Descriptor.Name)
			}
		}
		return names, nil
	}})
}

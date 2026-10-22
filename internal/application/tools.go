package application

import (
	"context"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type ToolRequirements = workspace.ExternalToolRequirements

// RequiredTools resolves configuration without parsing sources or loading runtimes.
func RequiredTools(ctx context.Context, options Options, paths []string) (ToolRequirements, error) {
	if _, err := repoconfig.CompileIgnorePatterns(options.IgnorePatterns); err != nil {
		return ToolRequirements{}, err
	}
	var names []string
	for _, registration := range options.Analyzers {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Descriptor.Name)
	}
	session, err := workspace.New(workspace.Options{Context: ctx, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, ReadFile: options.ReadFile, RuleNames: names})
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

package checking

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

// RequiredTools neither parses sources nor loads runtimes.
func RequiredTools(ctx context.Context, options Options, paths []string) (workspace.ExternalToolRequirements, error) {
	options, err := ConfigureToolIntegrations(options)
	if err != nil {
		return nil, err
	}
	if _, err := configuration.CompileIgnorePatterns(options.IgnorePatterns); err != nil {
		return workspace.ExternalToolRequirements{}, err
	}
	checks, err := registerChecks(options.Analyzers, options.Tools)
	if err != nil {
		return nil, err
	}
	session, err := workspace.New(ctx, workspace.Options{WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, NoIgnore: options.NoIgnore, ReadFile: options.ReadFile, ConfigConnections: options.ConfigConnections, RuleNames: checks.names, Rules: checks.rules})
	if err != nil {
		return workspace.ExternalToolRequirements{}, err
	}
	providers := checks.toolNames()
	return session.RequiredTools(ctx, paths, workspace.ToolOptions{Presets: options.Presets, Required: func(cfg, project *configuration.Config) ([]string, error) {
		if err := configuration.ValidateToolNames(project, providers...); err != nil {
			return nil, err
		}
		var names []string
		for _, r := range options.Tools {
			if r.NeedsProcess == nil {
				continue
			}
			owned, err := (contract.Context{Config: cfg, ProjectConfig: project}).Detached()
			if err != nil {
				return nil, err
			}
			needed, err := r.NeedsProcess(ctx, owned.Config, owned.ProjectConfig)
			if err != nil {
				return nil, err
			}
			if needed {
				names = append(names, r.Name)
			}
		}
		return names, nil
	}})
}

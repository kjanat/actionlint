package checking

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

type ToolRequirements = workspace.ExternalToolRequirements

// toolNames lists the rule names of registered tool providers. Unlike the
// rule names configuration loading binds, it holds no document analyzer, so
// tools.<name> entries bind to tool providers only.
func toolNames(registrations []tools.Registration) []string {
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.Descriptor.Name)
	}
	return names
}

// RequiredTools resolves configuration without parsing sources or loading runtimes.
func RequiredTools(ctx context.Context, options Options, paths []string) (ToolRequirements, error) {
	options, err := ConfigureToolIntegrations(options)
	if err != nil {
		return nil, err
	}
	if _, err := repoconfig.CompileIgnorePatterns(options.IgnorePatterns); err != nil {
		return ToolRequirements{}, err
	}
	var names []string
	registrations := configurationRegistrations(options.Analyzers, options.Tools)
	for _, registration := range options.Analyzers {
		if !validation.IsNil(registration.Checker) {
			names = append(names, registration.Checker.Descriptor().Name)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Descriptor.Name)
	}
	rules, err := ConfigurationRules().WithRegisteredRules(registrations...)
	if err != nil {
		return nil, err
	}
	session, err := workspace.New(ctx, workspace.Options{WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, NoIgnore: options.NoIgnore, ReadFile: options.ReadFile, ConfigConnections: options.ConfigConnections, RuleNames: names, Rules: rules})
	if err != nil {
		return ToolRequirements{}, err
	}
	providers := toolNames(options.Tools)
	return session.RequiredTools(ctx, paths, workspace.ToolOptions{Presets: options.Presets, Required: func(cfg, project *repoconfig.Config) ([]string, error) {
		if err := repoconfig.ValidateToolNames(project, providers...); err != nil {
			return nil, err
		}
		var names []string
		for _, r := range options.Tools {
			if r.NeedsProcess == nil {
				continue
			}
			needed, err := r.NeedsProcess(ctx, cfg, project)
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

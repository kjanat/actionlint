package checking

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs/validation"
)

// toolNames lists the rule names of registered tool providers. Unlike the
// rule names configuration loading binds, it holds no document analyzer, so
// tools.<name> entries bind to tool providers only.
func toolNames(registrations []contract.Registration) []string {
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		names = append(names, registration.Name)
	}
	return names
}

// RequiredTools resolves configuration without parsing sources or loading runtimes.
func RequiredTools(ctx context.Context, options Options, paths []string) (workspace.ExternalToolRequirements, error) {
	options, err := ConfigureToolIntegrations(options)
	if err != nil {
		return nil, err
	}
	if _, err := configuration.CompileIgnorePatterns(options.IgnorePatterns); err != nil {
		return workspace.ExternalToolRequirements{}, err
	}
	var names []string
	registrations := ConfigurationRegistrations(options.Analyzers, options.Tools)
	for _, registration := range options.Analyzers {
		if !validation.IsNil(registration.Checker) {
			names = append(names, registration.Checker.Descriptor().Name)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Name)
	}
	rules, err := ConfigurationRules().WithRegisteredRules(registrations...)
	if err != nil {
		return nil, err
	}
	session, err := workspace.New(ctx, workspace.Options{WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, NoIgnore: options.NoIgnore, ReadFile: options.ReadFile, ConfigConnections: options.ConfigConnections, RuleNames: names, Rules: rules})
	if err != nil {
		return workspace.ExternalToolRequirements{}, err
	}
	providers := toolNames(options.Tools)
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

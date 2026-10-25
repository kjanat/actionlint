package application

import (
	"context"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/rule"
)

type ToolRequirements = workspace.ExternalToolRequirements

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
	var descriptors []rule.Descriptor
	for _, registration := range options.Analyzers {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
			descriptors = append(descriptors, registration.Analyzer.Descriptor().Descriptor)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Descriptor.Name)
		if _, builtin := rulecatalog.Find(registration.Descriptor.Name); !builtin || registration.ToolIntegration != nil {
			descriptors = append(descriptors, registration.Descriptor)
		}
	}
	session, err := workspace.New(workspace.Options{Context: ctx, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig, ReadFile: options.ReadFile, RuleNames: names})
	if err != nil {
		return ToolRequirements{}, err
	}
	return session.RequiredTools(ctx, paths, workspace.ToolOptions{Presets: options.Presets, Descriptors: descriptors, Required: func(cfg, project *repoconfig.Config, inside bool) ([]string, error) {
		var names []string
		for _, r := range options.Tools {
			if r.NeedsProcess == nil {
				continue
			}
			needed, err := r.NeedsProcess(ctx, cfg, project, inside)
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

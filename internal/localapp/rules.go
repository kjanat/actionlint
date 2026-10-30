package localapp

import (
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"context"
	"errors"
	"io"

	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"
)

// Rules discovers selected providers without analyzing a document.
func Rules(ctx context.Context, paths []string) (result []rule.Descriptor, err error) {
	return RulesSources(ctx, PluginSources{Paths: paths})
}

// RulesSources lists managed document replacements under their existing tool
// rule. Explicit standalone components keep their independently declared rule.
func RulesSources(ctx context.Context, sources PluginSources) (result []rule.Descriptor, err error) {
	plugins, err := loadPlugins(ctx, sources, io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	if _, err := plugins.configureManaged(application.Options{Tools: registry.Registrations(nil), Analyzers: plugins.registrations()}); err != nil {
		return nil, err
	}
	var extensions []rule.Descriptor
	for _, plugin := range plugins.plugins {
		if !plugin.HasAnalyzer() {
			continue
		}
		if plugin.ToolIntegration() != nil {
			continue
		}
		extensions = append(extensions, plugin.Descriptor().Descriptor)
	}
	result, err = rulecatalog.Combine(extensions...)
	if err != nil {
		return nil, err
	}
	for _, plugin := range plugins.plugins {
		if integration := plugin.ToolIntegration(); integration != nil {
			for i := range result {
				if result[i].Name == integration.Tool {
					result[i] = plugin.Descriptor().Descriptor
				}
			}
		}
	}
	seen := make(map[string]bool)
	for _, engine := range plugins.engines {
		descriptor := engine.descriptor.Rule.Clone()
		if seen[descriptor.Name] {
			return nil, errors.New("duplicate script engine: " + descriptor.Name)
		}
		seen[descriptor.Name] = true
		replaced := false
		for i := range result {
			if result[i].Name == descriptor.Name {
				if builtin, ok := rulecatalog.Find(descriptor.Name); !ok || builtin.Provider == "actionlint" {
					return nil, errors.New("script engine conflicts with analyzer: " + descriptor.Name)
				}
				result[i] = descriptor
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, descriptor)
		}
	}
	return result, nil
}

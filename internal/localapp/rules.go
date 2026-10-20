package localapp

import (
	"context"
	"errors"
	"io"

	"actionlint.kjanat.dev/v2/internal/plugins/core"
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
	plugins, err := loadPlugins(ctx, sources.Paths, io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	var extensions []rule.Descriptor
	for _, plugin := range plugins.plugins {
		if !plugin.HasAnalyzer() {
			continue
		}
		managed := false
		for path := range sources.managed {
			managed = managed || plugins.analyzerSources[path] == plugin
		}
		if managed {
			continue
		}
		extensions = append(extensions, plugin.Descriptor().Descriptor)
	}
	result, err = rulecatalog.Combine(extensions...)
	if err != nil {
		return nil, err
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

func engineDescriptor(metadata core.Metadata) rule.Descriptor {
	descriptor := rule.Descriptor{Name: metadata.Engine.Name, Description: metadata.Name, Provider: metadata.ID}
	for _, candidate := range metadata.Rules {
		if candidate.Name == descriptor.Name {
			return candidate.Clone()
		}
	}
	return descriptor
}

package assembly

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func RulesWithPluginPaths(ctx context.Context, paths []string, acquisition dependencycache.Acquisition) (result []rule.Descriptor, err error) {
	sources, err := pluginselection.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return RulesWithPlugins(ctx, sources)
}

func RulesWithPlugins(ctx context.Context, sources pluginselection.Sources) (result []rule.Descriptor, err error) {
	plugins, _, err := loadDeclarations(ctx, sources)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.Close(context.WithoutCancel(ctx))) }()
	var extensions []rule.Descriptor
	for _, local := range plugins.Providers {
		if local.Files.Manifest.Role == toolmanifest.RoleDocument {
			extensions = append(extensions, local.Files.Manifest.Rule.Clone())
		}
	}
	for _, plugin := range plugins.Components {
		if !plugin.HasAnalyzer() {
			continue
		}
		if plugin.ToolIntegration() != nil {
			continue
		}
		extensions = append(extensions, plugin.Descriptor().Descriptor)
	}
	result, err = checking.CombineRuleDeclarations(extensions...)
	if err != nil {
		return nil, err
	}
	for _, plugin := range plugins.Components {
		if integration := plugin.ToolIntegration(); integration != nil {
			for i := range result {
				if result[i].Name == integration.Tool {
					result[i] = plugin.Descriptor().Descriptor
				}
			}
		}
	}
	seen := make(map[string]bool)
	for _, engine := range plugins.ScriptEngines {
		descriptor := engine.Descriptor.Rule.Clone()
		if seen[descriptor.Name] {
			return nil, errors.New("duplicate script engine: " + descriptor.Name)
		}
		seen[descriptor.Name] = true
		replaced := false
		for i := range result {
			if result[i].Name == descriptor.Name {
				if builtin, ok := checking.FindRule(descriptor.Name); !ok || builtin.Provider == "actionlint" {
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

package checking

import (
	"context"
	"errors"
	"io"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// RulesWithPluginPaths discovers selected providers without analyzing a document.
func RulesWithPluginPaths(ctx context.Context, paths []string, acquisition plugininstall.Acquisition) (result []rule.Descriptor, err error) {
	sources, err := plugininstall.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return RulesWithPlugins(ctx, sources)
}

// RulesWithPlugins lists managed document replacements under their existing tool
// rule. Explicit standalone components keep their independently declared rule.
func RulesWithPlugins(ctx context.Context, sources plugininstall.Sources) (result []rule.Descriptor, err error) {
	plugins, err := pluginexec.Load(ctx, sources, BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.Close(context.WithoutCancel(ctx))) }()
	if _, err := ConfigureManagedPlugins(plugins, Options{Tools: registry.Registrations(nil), Analyzers: plugins.Registrations()}); err != nil {
		return nil, err
	}
	var extensions []rule.Descriptor
	for _, local := range plugins.Providers {
		if local.Files.Manifest.Role == manifests.RoleDocument {
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
	result, err = CombineRuleDeclarations(extensions...)
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
				if builtin, ok := FindRule(descriptor.Name); !ok || builtin.Provider == "actionlint" {
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

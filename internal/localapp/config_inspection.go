package localapp

import (
	"context"
	"errors"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// InspectConfig validates rule names against explicitly selected or project plugins.
func InspectConfig(ctx context.Context, selection config.Selection, paths []string, origins bool) (inspection config.Inspection, err error) {
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	path, err := config.SelectedPath(selection)
	if err != nil {
		return inspection, err
	}
	inspection.Path = path
	directory, err := os.Getwd()
	if err != nil {
		return inspection, err
	}
	sources, err := (PluginSelection{Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled}).Resolve(ctx, directory, application.Input{Kind: application.Repository})
	if err != nil {
		return inspection, err
	}
	plugins, err := loadPlugins(ctx, sources, io.Discard, false)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	rules := make([]string, 0, len(plugins.plugins)+len(plugins.engines))
	for _, plugin := range plugins.plugins {
		if plugin.HasAnalyzer() {
			rules = append(rules, plugin.Descriptor().Name)
		}
	}
	for _, engine := range plugins.engines {
		rules = append(rules, engine.descriptor.Rule.Name)
	}
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	return config.Inspect(selection, origins, rules...)
}

// InstalledProviders returns the tool provider manifests among the plugins
// that the --plugin flags or the selected configuration name: local manifest
// files and the manifests of oci:// sources. It runs no plugin and downloads
// no executable; other plugins are resolved but skipped.
func InstalledProviders(ctx context.Context, selection config.Selection, paths []string) ([]provider.Manifest, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sources, err := (PluginSelection{Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled}).Resolve(ctx, directory, application.Input{Kind: application.Repository})
	if err != nil {
		return nil, err
	}
	var manifests []provider.Manifest
	for _, path := range sources.Paths {
		if provider.IsLocalManifest(path) {
			manifest, _, err := provider.ReadLocal(fsread.New(ctx, nil, nil), path)
			if err != nil {
				return nil, err
			}
			manifests = append(manifests, manifest)
			continue
		}
		artifact := sources.artifacts[path]
		if artifact == nil {
			if err := pluginartifact.RequireRemotePin(path); err != nil {
				return nil, err
			}
			if artifact, err = pluginartifact.Resolve(ctx, path, artifactOptions()); err != nil {
				return nil, err
			}
		}
		if artifact.Info.Kind != pluginartifact.ProviderManifest {
			continue
		}
		manifest, err := pulledProvider(artifact)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

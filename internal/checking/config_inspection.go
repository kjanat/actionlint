package checking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/plugins/manifests"
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
	sources, err := (plugininstall.Selection{RuleSet: ConfigurationRules(), Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled}).Resolve(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return inspection, err
	}
	plugins, err := pluginexec.Load(ctx, sources, BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, plugins.Close(context.WithoutCancel(ctx))) }()
	rules := make([]rule.Descriptor, 0, len(plugins.Components)+len(plugins.ScriptEngines)+len(plugins.Providers))
	for _, plugin := range plugins.Components {
		if plugin.HasAnalyzer() {
			rules = append(rules, plugin.Descriptor().Descriptor)
		}
	}
	for _, engine := range plugins.ScriptEngines {
		rules = append(rules, engine.Descriptor.Rule)
	}
	for _, provider := range plugins.Providers {
		rules = append(rules, provider.Files.Manifest.Rule)
	}
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	return ConfigurationRules().InspectContextWithRules(ctx, selection, origins, rules...)
}

// InstalledProviders returns the tool provider manifests among the plugins
// that the --plugin flags or the selected configuration name: local manifest
// files and the manifests of oci:// sources. It runs no plugin and downloads
// no executable; other plugins are resolved but skipped.
func InstalledProviders(ctx context.Context, selection config.Selection, paths []string) ([]pluginmanifests.Manifest, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sources, err := (plugininstall.Selection{RuleSet: ConfigurationRules(), Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled}).Resolve(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return nil, err
	}
	var manifests []pluginmanifests.Manifest
	for _, path := range sources.Paths {
		if pluginmanifests.IsLocalManifest(path) {
			manifest, programs, err := pluginmanifests.ReadLocal(files.New(ctx, nil, nil), path)
			if err != nil {
				return nil, err
			}
			if err := process.ValidateManifest(manifest, programs); err != nil {
				return nil, fmt.Errorf("plugin %s: %w", path, err)
			}
			manifests = append(manifests, manifest)
			continue
		}
		artifact := sources.Artifacts[path]
		if artifact == nil {
			if err := pluginartifact.RequireRemotePin(path); err != nil {
				return nil, err
			}
			if artifact, err = pluginartifact.Resolve(ctx, path, sources.ArtifactOptions(path)); err != nil {
				return nil, err
			}
		}
		if artifact.Info.Kind != pluginartifact.ProviderManifest {
			continue
		}
		if err := plugininstall.ValidateProviderArtifact(ctx, artifact, sources.ArtifactOptions(path)); err != nil {
			return nil, err
		}
		manifest, err := plugininstall.ReadProviderArtifact(artifact)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

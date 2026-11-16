package checking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	pluginmanifests "actionlint.kjanat.dev/v2/internal/model/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
)

// InspectConfig validates rule names against explicitly selected or project plugins.
func InspectConfig(ctx context.Context, selection configuration.ConfigFileSelection, paths []string, origins bool, acquisition pluginacquisition.Settings) (inspection configreport.Inspection, err error) {
	ctx = source.WithReadCache(ctx)
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	path, err := configuration.SelectedPath(selection)
	if err != nil {
		return inspection, err
	}
	inspection.Path = path
	directory, err := os.Getwd()
	if err != nil {
		return inspection, err
	}
	sources, err := (pluginselection.PluginSelection{RuleSet: ConfigurationRules(), Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled, Acquisition: acquisition}).Resolve(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return inspection, err
	}
	plugins, err := pluginexec.LoadPlugins(ctx, sources, BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		return inspection, err
	}
	defer func() { err = errors.Join(err, plugins.Close(context.WithoutCancel(ctx))) }()
	configured, err := ConfigureManagedPlugins(plugins, Options{Tools: registry.Registrations(nil), Analyzers: plugins.Registrations()})
	if err != nil {
		return inspection, err
	}
	rules := configurationRegistrations(configured.Analyzers, configured.Tools)
	for _, engine := range plugins.ScriptEngines {
		rules = append(rules, configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: engine.Descriptor.Rule, Scope: rule.SourceConfiguration, SupportsActions: true}, ReplacesTool: engine.Descriptor.Engine})
	}
	for _, provider := range plugins.Providers {
		manifest := provider.Files.Manifest
		scope, scopeErr := manifest.Scope.ConfigurationScope()
		if scopeErr != nil {
			return inspection, fmt.Errorf("provider %s: %w", manifest.ID, scopeErr)
		}
		registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: manifest.Rule, Scope: scope, SupportsActions: true}, ToolSettings: new(manifest.Settings())}
		if manifest.Role == pluginmanifests.RoleScript {
			registration.ReplacesTool = manifest.Rule.Name
		}
		rules = append(rules, registration)
	}
	if err := ctx.Err(); err != nil {
		return inspection, err
	}
	reader := source.New(ctx, nil, acquisition.Connections)
	policy, err := configuration.LoadNetworkPolicy(ctx, acquisition.Connections)
	if err != nil {
		return inspection, err
	}
	return ConfigurationRules().InspectContextWithRegistrations(ctx, selection, origins, reader.ReadFile, policy, rules...)
}

// InstalledProviders returns the tool provider manifests among the plugins
// that the --plugin flags or the selected configuration name: local manifest
// files and the manifests of oci:// sources. It runs no plugin and downloads
// no executable; other plugins are resolved but skipped.
func InstalledProviders(ctx context.Context, selection configuration.ConfigFileSelection, paths []string, acquisition pluginacquisition.Settings) ([]pluginmanifests.Manifest, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sources, err := (pluginselection.PluginSelection{RuleSet: ConfigurationRules(), Paths: paths, ConfigFile: selection.Path, Disabled: selection.Disabled, Acquisition: acquisition}).Resolve(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository})
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
			if err := artifacts.RequireRemotePin(path); err != nil {
				return nil, err
			}
			if artifact, err = artifacts.Resolve(ctx, path, sources.ArtifactOptions(path)); err != nil {
				return nil, err
			}
		}
		if artifact.Info.Kind != artifacts.ProviderManifest {
			continue
		}
		if err := process.ValidateProviderArtifact(ctx, artifact, sources.ArtifactOptions(path)); err != nil {
			return nil, err
		}
		manifest, err := process.ReadProviderArtifact(artifact)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

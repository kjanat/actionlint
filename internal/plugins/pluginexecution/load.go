package pluginexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/localproviders"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
	"actionlint.kjanat.dev/v2/internal/plugins/toolproviders"
)

type Loaded struct {
	Processes     *hostprocess.ProcessPool
	Components    []*component.Plugin
	ScriptEngines []ScriptEngineRegistration
	Modules       []*module.Engine
	deferred      []*deferredScriptEngine
	Providers     []SelectedProvider
	NamedTools    []string
	SkippedTools  []string
	ToolIDs       []string
	Acquisition   dependencycache.Acquisition
	DisableTools  bool
}

type SelectedProvider struct {
	Files      toolproviders.ProviderFiles
	Network    acquisitionhttp.Grant
	Repository bool
}
type ScriptEngineRegistration struct {
	Backend    scriptengine.Backend
	Descriptor scriptengine.Descriptor
}

func LoadPlugins(ctx context.Context, sources pluginselection.Sources, reserved rule.Names, log io.Writer, debug bool) (*Loaded, error) {
	loaded := &Loaded{NamedTools: sources.NamedTools, SkippedTools: slices.Clone(sources.SkippedTools), Acquisition: sources.Acquisition, DisableTools: sources.DisableTools}
	if sources.Releases != nil {
		for _, pin := range sources.Releases.Plugins {
			loaded.ToolIDs = append(loaded.ToolIDs, pin.Name)
			if pin.Alias != "" {
				loaded.ToolIDs = append(loaded.ToolIDs, pin.Alias)
			}
		}
	}
	for _, path := range sources.Paths {
		if pin, lazy := sources.LazySources[path]; lazy && !sources.DisableTools {
			ids, err := MetadataTools([]byte(pin.Metadata))
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			loaded.ToolIDs = append(loaded.ToolIDs, ids...)
			if index := slices.IndexFunc(ids, func(id string) bool { return slices.Contains(sources.DisabledTools, id) }); index >= 0 {
				loaded.SkippedTools = append(loaded.SkippedTools, ids[index])
				continue
			}
			engine, err := newDeferredScriptEngine(pin, pluginregistry.StatusOptions{
				Acquisition: sources.Acquisition, Network: sources.DefaultRegistryNetwork,
			}, reserved)
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			loaded.deferred = append(loaded.deferred, engine)
			loaded.ScriptEngines = append(loaded.ScriptEngines, ScriptEngineRegistration{engine, engine.ScriptDescriptor()})
			continue
		}
		networkPolicy := sources.NetworkGrants[path]
		if toolmanifest.IsLocalManifest(path) {
			if sources.DisableTools {
				continue
			}
			// A local manifest names a command found on PATH, and nothing is fetched.
			manifest, programs, err := localproviders.Read(files.New(ctx, nil, nil), path)
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.DisabledTools, manifest.Rule.Name) {
				loaded.SkippedTools = append(loaded.SkippedTools, manifest.Rule.Name)
				continue
			}
			loaded.Providers = append(loaded.Providers, SelectedProvider{Files: toolproviders.ProviderFiles{Manifest: manifest, Programs: programs}, Repository: sources.Configured})
			continue
		}
		if err := artifacts.RequireRemotePin(path); err != nil {
			return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
		}
		artifact := sources.Artifacts[path]
		if artifact == nil {
			var err error
			resolveCtx := ctx
			if sources.DisableTools {
				options := network.Settings(ctx)
				options.Offline, options.WarmCache = true, false
				resolveCtx, _ = network.WithSession(ctx, options)
			}
			artifact, err = artifacts.Resolve(resolveCtx, path, sources.ArtifactOptions(path))
			if err != nil {
				if sources.DisableTools && network.OnlyOffline(err) {
					continue
				}
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
		}
		path = artifact.Info.Source
		if sources.DisableTools && (artifact.Info.Kind == artifacts.Core || artifact.Info.Kind == artifacts.ProviderManifest) {
			continue
		}
		if artifact.Info.Kind == artifacts.ProviderManifest {
			manifest, err := toolproviders.ReadProviderArtifact(artifact)
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.DisabledTools, manifest.Rule.Name) {
				loaded.SkippedTools = append(loaded.SkippedTools, manifest.Rule.Name)
				continue
			}
			// configure fetches the executable and transform programs after
			// the --allow-config-processes check.
			loaded.Providers = append(loaded.Providers, SelectedProvider{Files: toolproviders.ProviderFiles{Manifest: manifest, Source: path}, Network: networkPolicy, Repository: sources.Configured})
			continue
		}
		if !sources.DisableTools && (len(sources.DisabledTools) != 0 || len(sources.NamedTools) != 0) {
			ids, err := MetadataTools(artifact.Info.Metadata)
			if err != nil {
				return nil, &LoadError{errors.Join(fmt.Errorf("plugin %s: %w", path, err), loaded.Close(context.WithoutCancel(ctx)))}
			}
			if index := slices.IndexFunc(ids, func(id string) bool { return slices.Contains(sources.DisabledTools, id) }); index >= 0 {
				loaded.SkippedTools = append(loaded.SkippedTools, ids[index])
				continue
			}
			loaded.ToolIDs = append(loaded.ToolIDs, ids...)
		}
		if artifact.Info.Kind == artifacts.Core {
			engine, err := module.LoadArtifact(ctx, artifact, module.Options{CacheDir: pluginacquisition.CompiledModuleCacheDir(sources.Acquisition)})
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			loaded.Modules = append(loaded.Modules, engine)
			loaded.ScriptEngines = append(loaded.ScriptEngines, ScriptEngineRegistration{engine, engine.ScriptDescriptor()})

			continue
		}
		options := component.Options{ReservedRules: reserved}
		if debug {
			name := filepath.Base(path)
			options.Log = func(line string) {
				_, _ = fmt.Fprintf(log, "[plugin %s] %s\n", name, line)
			}
		}
		plugin, err := component.LoadArtifact(ctx, artifact, options)
		if err != nil {
			return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
		}
		if debug {
			_, _ = fmt.Fprintf(log, "[Linter] Loaded component from %s (%s)\n", path, plugin.Limits())
		}
		loaded.Components = append(loaded.Components, plugin)
		if plugin.HasScriptEngine() && !sources.DisableTools {
			loaded.ScriptEngines = append(loaded.ScriptEngines, ScriptEngineRegistration{plugin, plugin.ScriptDescriptor()})
		}
	}
	return loaded, nil
}

func (l *Loaded) Registrations() []results.AnalyzerRegistration {
	registrations := make([]results.AnalyzerRegistration, 0, len(l.Components))
	if l.DisableTools {
		return registrations
	}
	for _, plugin := range l.Components {
		if !plugin.HasAnalyzer() {
			continue
		}
		registrations = append(registrations, results.AnalyzerRegistration{Analyzer: plugin})
	}
	return registrations
}

func (l *Loaded) RunnerProviders() []runnerapi.Registration {
	var registrations []runnerapi.Registration
	for _, plugin := range l.Components {
		if plugin.HasRunnerProvider() {
			registrations = append(registrations, runnerapi.Registration{Provider: plugin, ImplementationID: plugin.ArtifactDigest()})
		}
	}
	return registrations
}

func (l *Loaded) BeginAnalysis(ctx context.Context) ([]ScriptEngineRegistration, error) {
	now := time.Now()
	var changed []ScriptEngineRegistration
	for _, engine := range l.deferred {
		if err := engine.beginAnalysis(ctx, now); err != nil {
			return changed, err
		}
		descriptor := engine.ScriptDescriptor()
		for i, registration := range l.ScriptEngines {
			if registration.Backend == scriptengine.Backend(engine) && !sameScriptDescriptor(registration.Descriptor, descriptor) {
				l.ScriptEngines[i].Descriptor = descriptor
				changed = append(changed, l.ScriptEngines[i])
			}
		}
	}
	return changed, nil
}

func (l *Loaded) Close(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	var err error
	if l.Processes != nil {
		l.Processes.Wait()
	}
	for _, plugin := range l.Components {
		err = errors.Join(err, plugin.Close(ctx))
	}
	for _, engine := range l.Modules {
		err = errors.Join(err, engine.Close(ctx))
	}
	for _, engine := range l.deferred {
		err = errors.Join(err, engine.Close(ctx))
	}
	l.deferred = nil
	l.Modules = nil
	l.ScriptEngines = nil
	l.Components = nil
	l.Providers = nil
	return err
}

// MetadataTools reads static plugin metadata without running the plugin.
func MetadataTools(metadata json.RawMessage) ([]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	var declared struct {
		Rules []struct {
			Name string `json:"name"`
		} `json:"rules"`
		Engine struct {
			Name string `json:"name"`
		} `json:"engine"`
	}
	if err := json.Unmarshal(metadata, &declared); err != nil {
		return nil, fmt.Errorf("read plugin metadata: %w", err)
	}
	var names []string
	if declared.Engine.Name != "" {
		names = append(names, declared.Engine.Name)
	}
	for _, rule := range declared.Rules {
		if rule.Name != "" {
			names = append(names, rule.Name)
		}
	}
	return names, nil
}

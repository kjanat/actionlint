package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/files"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/component"

	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

type Loaded struct {
	Processes     *hostprocess.ProcessPool
	Components    []*component.Plugin
	ScriptEngines []ScriptEngineRegistration
	Modules       []*corewasm.Engine
	// Providers contains process tool manifests read from local paths or OCI sources.
	Providers []SelectedProvider
	// NamedTools lists tool IDs selected by the invocation. configure checks each ID.
	NamedTools []string
	// SkippedTools lists disabled tool IDs that matched plugins that were not loaded.
	SkippedTools []string
	// ToolIDs lists names that --no-tool can disable: lock pin names and aliases,
	// and engine and rule names from loaded plugin metadata.
	ToolIDs     []string
	Acquisition plugininstall.Acquisition
}

// SelectedProvider records the files and execution authority of one selected process plugin.
type SelectedProvider struct {
	Files      process.ProviderFiles
	Network    acquisitionhttp.Grant
	Repository bool
}
type ScriptEngineRegistration struct {
	Backend    checkinputs.ScriptBackend
	Descriptor checkinputs.ScriptEngineDescriptor
}

func Load(ctx context.Context, sources plugininstall.Sources, reserved rule.Names, log io.Writer, debug bool) (*Loaded, error) {
	loaded := &Loaded{NamedTools: sources.NamedTools, SkippedTools: slices.Clone(sources.SkippedTools), Acquisition: sources.Acquisition}
	if sources.Releases != nil {
		for _, pin := range sources.Releases.Plugins {
			loaded.ToolIDs = append(loaded.ToolIDs, pin.Name)
			if pin.Alias != "" {
				loaded.ToolIDs = append(loaded.ToolIDs, pin.Alias)
			}
		}
	}
	for _, path := range sources.Paths {
		network := sources.NetworkGrants[path]
		if manifests.IsLocalManifest(path) {
			// A local manifest names a command found on PATH; nothing is fetched.
			manifest, programs, err := manifests.ReadLocal(files.New(ctx, nil, nil), path)
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.DisabledTools, manifest.Rule.Name) {
				loaded.SkippedTools = append(loaded.SkippedTools, manifest.Rule.Name)
				continue
			}
			loaded.Providers = append(loaded.Providers, SelectedProvider{Files: process.ProviderFiles{Manifest: manifest, Programs: programs}, Repository: sources.Configured})
			continue
		}
		if err := pluginartifact.RequireRemotePin(path); err != nil {
			return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
		}
		artifact := sources.Artifacts[path]
		if artifact == nil {
			var err error
			artifact, err = pluginartifact.Resolve(ctx, path, sources.ArtifactOptions(path))
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
		}
		path = artifact.Info.Source
		if artifact.Info.Kind == pluginartifact.ProviderManifest {
			manifest, err := plugininstall.ReadProviderArtifact(artifact)
			if err != nil {
				return nil, &LoadError{errors.Join(err, loaded.Close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.DisabledTools, manifest.Rule.Name) {
				loaded.SkippedTools = append(loaded.SkippedTools, manifest.Rule.Name)
				continue
			}
			// configure fetches the executable and transform programs after
			// the --allow-config-processes check.
			loaded.Providers = append(loaded.Providers, SelectedProvider{Files: process.ProviderFiles{Manifest: manifest, Source: path}, Network: network, Repository: sources.Configured})
			continue
		}
		// Metadata is read only when --tool or --no-tool named an id, so
		// unusual metadata cannot fail a run that names none.
		if len(sources.DisabledTools) != 0 || len(sources.NamedTools) != 0 {
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
		if artifact.Info.Kind == pluginartifact.Core {
			engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: sources.Acquisition.CompiledModuleCacheDir()})
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
		if plugin.HasScriptEngine() {
			loaded.ScriptEngines = append(loaded.ScriptEngines, ScriptEngineRegistration{plugin, plugin.ScriptDescriptor()})
		}
	}
	return loaded, nil
}

func (l *Loaded) Registrations() []checkinputs.Registration {
	registrations := make([]checkinputs.Registration, 0, len(l.Components))
	for _, plugin := range l.Components {
		if !plugin.HasAnalyzer() {
			continue
		}
		registrations = append(registrations, checkinputs.Registration{Checker: plugin})
	}
	return registrations
}

func (l *Loaded) RunnerProviders() []runner.Registration {
	var registrations []runner.Registration
	for _, plugin := range l.Components {
		if plugin.HasRunnerProvider() {
			registrations = append(registrations, runner.Registration{Provider: plugin, ImplementationID: plugin.ArtifactDigest()})
		}
	}
	return registrations
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
	l.Modules = nil
	l.ScriptEngines = nil
	l.Components = nil
	l.Providers = nil
	return err
}

// MetadataTools returns the engine name and rule names that static plugin
// metadata declares. It reads the inspected metadata without running the
// plugin.
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

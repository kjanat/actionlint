package localapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/fsread"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/runner"
)

type loadedPlugins struct {
	plugins []*component.Plugin
	engines []scriptEngineRegistration
	core    []*corewasm.Engine
	// providers are local process tool provider manifests.
	providers []localProvider
	// tools lists the tool ids the invocation named; configure checks that
	// each names a tool.
	tools []string
	// skipped lists the disabled tool ids that matched a plugin, which was
	// therefore not loaded.
	skipped []string
}

// localProvider is a tool provider manifest read from a local path.
type localProvider struct {
	manifest provider.Manifest
	programs provider.Programs
}

type scriptEngineRegistration struct {
	backend    analyzer.ScriptBackend
	descriptor analyzer.ScriptEngineDescriptor
}

func loadPlugins(ctx context.Context, sources PluginSources, log io.Writer, debug bool) (*loadedPlugins, error) {
	loaded := &loadedPlugins{tools: sources.tools, skipped: slices.Clone(sources.skipped)}
	for _, path := range sources.Paths {
		if provider.IsLocalManifest(path) {
			// A local manifest names a command found on PATH; nothing is fetched.
			manifest, programs, err := provider.ReadLocal(fsread.New(ctx, nil, nil), path)
			if err != nil {
				return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.disabledTools, manifest.Rule.Name) {
				loaded.skipped = append(loaded.skipped, manifest.Rule.Name)
				continue
			}
			loaded.providers = append(loaded.providers, localProvider{manifest, programs})
			continue
		}
		if err := pluginartifact.RequireRemotePin(path); err != nil {
			return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
		}
		artifact := sources.artifacts[path]
		if artifact == nil {
			var err error
			artifact, err = pluginartifact.Resolve(ctx, path, artifactOptions())
			if err != nil {
				return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
			}
		}
		path = artifact.Info.Source
		if id, err := disabledMetadataTool(artifact.Info.Metadata, sources.disabledTools); err != nil {
			return nil, &PluginLoadError{errors.Join(fmt.Errorf("plugin %s: %w", path, err), loaded.close(context.WithoutCancel(ctx)))}
		} else if id != "" {
			loaded.skipped = append(loaded.skipped, id)
			continue
		}
		if artifact.Info.Kind == pluginartifact.Core {
			engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: engineCacheDir()})
			if err != nil {
				return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
			}
			metadata := engine.Metadata()
			loaded.core = append(loaded.core, engine)
			loaded.engines = append(loaded.engines, scriptEngineRegistration{engine, metadata.ScriptDescriptor()})

			continue
		}
		var options component.Options
		if debug {
			name := filepath.Base(path)
			options.Log = func(line string) {
				_, _ = fmt.Fprintf(log, "[plugin %s] %s\n", name, line)
			}
		}
		plugin, err := component.LoadArtifact(ctx, artifact, options)
		if err != nil {
			return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
		}
		if debug {
			_, _ = fmt.Fprintf(log, "[Linter] Loaded component from %s (%s)\n", path, plugin.Limits())
		}
		loaded.plugins = append(loaded.plugins, plugin)
		if plugin.HasScriptEngine() {
			loaded.engines = append(loaded.engines, scriptEngineRegistration{plugin, plugin.ScriptDescriptor()})
		}
	}
	return loaded, nil
}

func (l *loadedPlugins) registrations() []analyzer.Registration {
	registrations := make([]analyzer.Registration, 0, len(l.plugins))
	for _, plugin := range l.plugins {
		if !plugin.HasAnalyzer() {
			continue
		}
		registrations = append(registrations, analyzer.Registration{Analyzer: plugin})
	}
	return registrations
}

func (l *loadedPlugins) runnerProviders() []runner.Registration {
	var registrations []runner.Registration
	for _, plugin := range l.plugins {
		if plugin.HasRunnerProvider() {
			registrations = append(registrations, runner.Registration{Provider: plugin, Identity: plugin.ArtifactDigest()})
		}
	}
	return registrations
}

func (l *loadedPlugins) close(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	var err error
	for _, plugin := range l.plugins {
		err = errors.Join(err, plugin.Close(ctx))
	}
	for _, engine := range l.core {
		err = errors.Join(err, engine.Close(ctx))
	}
	l.core = nil
	l.engines = nil
	l.plugins = nil
	l.providers = nil
	return err
}

// ScriptEngineLoader loads configured core modules and script-engine components
// with the same artifact verification and lifecycle for every native entrypoint.
func ScriptEngineLoader(log io.Writer, debug bool) func(context.Context, string) (analyzer.ScriptBackend, error) {
	return func(ctx context.Context, path string) (analyzer.ScriptBackend, error) {
		if err := pluginartifact.RequireRemotePin(path); err != nil {
			return nil, err
		}
		artifact, err := pluginartifact.Resolve(ctx, path, artifactOptions())
		if err != nil {
			return nil, err
		}
		return loadScriptArtifact(ctx, artifact, log, debug)
	}
}

func loadScriptArtifact(ctx context.Context, artifact *pluginartifact.Artifact, log io.Writer, debug bool) (analyzer.ScriptBackend, error) {
	if artifact.Info.Kind == pluginartifact.Component {
		var options component.Options
		if debug {
			options.Log = func(line string) { _, _ = fmt.Fprintln(log, line) }
		}
		plugin, err := component.LoadArtifact(ctx, artifact, options)
		if err != nil {
			return nil, err
		}
		if !plugin.HasScriptEngine() {
			return nil, errors.Join(errors.New("component does not implement the script-engine role"), plugin.Close(context.WithoutCancel(ctx)))
		}
		if debug {
			descriptor := plugin.ScriptDescriptor()
			_, _ = fmt.Fprintf(log, "[Linter] Loaded script component %q (%s %s) from %s (%s)\n",
				descriptor.ID, descriptor.Engine, descriptor.EngineVersion, plugin.Path(), plugin.Limits())
		}
		return plugin, nil
	}
	engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: engineCacheDir()})
	if err != nil {
		return nil, err
	}
	if debug {
		metadata := engine.Metadata()
		_, _ = fmt.Fprintf(log, "[Linter] Loaded engine %q (%s %s) from %s (%s)\n", metadata.ID, metadata.Engine.Name, metadata.Engine.Version, engine.Path(), engine.Limits())
	}
	return engine, nil
}

func engineCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "actionlint", "wazero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

func artifactOptions() pluginartifact.Options {
	base, err := os.UserCacheDir()
	if err != nil {
		return pluginartifact.Options{}
	}
	return pluginartifact.Options{CacheDir: filepath.Join(base, "actionlint", "artifacts")}
}

// InspectPlugin resolves and reads metadata without creating a runtime.
func InspectPlugin(ctx context.Context, source string) (pluginartifact.Info, error) {
	artifact, err := pluginartifact.Resolve(ctx, source, artifactOptions())
	if err != nil {
		return pluginartifact.Info{}, err
	}
	return artifact.Info, nil
}

// disabledMetadataTool returns the disabled tool id that static plugin
// metadata names as the engine or as one of its rules, or "". It reads the
// inspected metadata without running the plugin.
func disabledMetadataTool(metadata json.RawMessage, disabled []string) (string, error) {
	if len(disabled) == 0 || len(metadata) == 0 {
		return "", nil
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
		return "", fmt.Errorf("read plugin metadata: %w", err)
	}
	names := []string{declared.Engine.Name}
	for _, rule := range declared.Rules {
		names = append(names, rule.Name)
	}
	for _, name := range names {
		if name != "" && slices.Contains(disabled, name) {
			return name, nil
		}
	}
	return "", nil
}

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
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/fsread"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/runner"
)

type loadedPlugins struct {
	plugins []*component.Plugin
	engines []scriptEngineRegistration
	core    []*corewasm.Engine
	// providers are process tool provider manifests read from a local path
	// or pulled from an oci:// source.
	providers []localProvider
	// tools lists the tool ids the invocation named; configure checks that
	// each names a tool.
	tools []string
	// skipped lists the disabled tool ids that matched a plugin, which was
	// therefore not loaded.
	skipped []string
	// ids lists the ids --no-tool can disable for each loaded plugin: the
	// name and alias of its lock pin, and the engine and rule names in its
	// metadata.
	ids []string
}

// localProvider is a process tool provider manifest read from a local path
// or pulled from an oci:// source.
type localProvider struct {
	manifest provider.Manifest
	// programs are the manifest's transform programs. For a pulled
	// manifest, fetch reads them.
	programs provider.Programs
	// executable is the path of the provider's executable materialized from
	// the artifact store by fetch. Empty runs the manifest's process entry
	// from PATH.
	executable string
	// source is the oci:// source of a pulled manifest, empty for a local one.
	source string
	// repository is set when repository configuration declared source, so
	// running it needs --allow-config-processes.
	repository bool
}

type scriptEngineRegistration struct {
	backend    analyzer.ScriptBackend
	descriptor analyzer.ScriptEngineDescriptor
}

func loadPlugins(ctx context.Context, sources PluginSources, log io.Writer, debug bool) (*loadedPlugins, error) {
	loaded := &loadedPlugins{tools: sources.tools, skipped: slices.Clone(sources.skipped)}
	if sources.lock != nil {
		for _, pin := range sources.lock.Plugins {
			loaded.ids = append(loaded.ids, pin.Name)
			if pin.Alias != "" {
				loaded.ids = append(loaded.ids, pin.Alias)
			}
		}
	}
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
			loaded.providers = append(loaded.providers, localProvider{manifest: manifest, programs: programs})
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
		if artifact.Info.Kind == pluginartifact.ProviderManifest {
			manifest, err := pulledProvider(artifact)
			if err != nil {
				return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
			}
			if slices.Contains(sources.disabledTools, manifest.Rule.Name) {
				loaded.skipped = append(loaded.skipped, manifest.Rule.Name)
				continue
			}
			// configure fetches the executable and transform programs after
			// the --allow-config-processes check.
			loaded.providers = append(loaded.providers, localProvider{manifest: manifest, source: path, repository: sources.repository})
			continue
		}
		// Metadata is read only when --tool or --no-tool named an id, so
		// unusual metadata cannot fail a run that names none.
		if len(sources.disabledTools) != 0 || len(sources.tools) != 0 {
			ids, err := metadataTools(artifact.Info.Metadata)
			if err != nil {
				return nil, &PluginLoadError{errors.Join(fmt.Errorf("plugin %s: %w", path, err), loaded.close(context.WithoutCancel(ctx)))}
			}
			if index := slices.IndexFunc(ids, func(id string) bool { return slices.Contains(sources.disabledTools, id) }); index >= 0 {
				loaded.skipped = append(loaded.skipped, ids[index])
				continue
			}
			loaded.ids = append(loaded.ids, ids...)
		}
		if artifact.Info.Kind == pluginartifact.Core {
			engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: engineCacheDir()})
			if err != nil {
				return nil, &PluginLoadError{errors.Join(err, loaded.close(context.WithoutCancel(ctx)))}
			}
			loaded.core = append(loaded.core, engine)
			loaded.engines = append(loaded.engines, scriptEngineRegistration{engine, engine.ScriptDescriptor()})

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
	if artifact.Info.Kind != pluginartifact.Core && artifact.Info.Kind != pluginartifact.Component {
		return nil, fmt.Errorf("%s is a %s artifact, not a WebAssembly module", artifact.Info.Source, artifact.Info.Kind)
	}
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

// pulledProvider parses the tool provider manifest layer of an oci://
// artifact. The manifest must use the process transport: a WebAssembly
// plugin is published as a wasm layer instead.
func pulledProvider(artifact *pluginartifact.Artifact) (provider.Manifest, error) {
	manifest, err := provider.Parse(artifact.Bytes)
	if err != nil {
		return provider.Manifest{}, fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
	}
	if manifest.Transport != provider.TransportProcess {
		return provider.Manifest{}, fmt.Errorf("plugin %s: tool provider %s uses the %s transport; an OCI tool provider manifest must use the process transport, and a WebAssembly plugin is published as a %s layer", artifact.Info.Source, manifest.Rule.Name, manifest.Transport, pluginartifact.MediaTypeWasm)
	}
	return manifest, nil
}

// fetch returns p with its transform programs and executable read from the
// layers of its oci:// source, through the artifact store. The executable
// is the one manifest.artifacts lists for this host, materialized from the
// store; without artifacts the process entry runs from PATH.
func (p localProvider) fetch(ctx context.Context) (localProvider, error) {
	options := artifactOptions()
	layer := func(digest string) ([]byte, error) {
		return oci.Layer(ctx, p.source, strings.TrimPrefix(digest, "sha256:"), options)
	}
	name := p.manifest.Rule.Name
	programs, err := provider.LoadPrograms(p.manifest, layer)
	if err != nil {
		return localProvider{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	p.programs = programs
	if len(p.manifest.Artifacts) == 0 {
		return p, nil
	}
	platform := pluginartifact.HostPlatform()
	index := slices.IndexFunc(p.manifest.Artifacts, func(a provider.Artifact) bool { return a.Platform == platform })
	if index < 0 {
		return localProvider{}, fmt.Errorf("tool provider %s has no executable for %s", name, platform)
	}
	executable := p.manifest.Artifacts[index]
	digest := strings.TrimPrefix(executable.Digest, "sha256:")
	store := pluginartifact.Store{Dir: options.CacheDir}
	p.executable, err = store.Materialize(ctx, digest, executable.Filename, artifactMaxBytes)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, pluginartifact.ErrCorrupt) {
		// Fetch a missing or half-written executable, then copy it again.
		if _, err = layer(executable.Digest); err == nil {
			p.executable, err = store.Materialize(ctx, digest, executable.Filename, artifactMaxBytes)
		}
	}
	if err != nil {
		return localProvider{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	return p, nil
}

// artifactMaxBytes bounds a stored transform program or executable, as
// pluginartifact.Options.MaxBytes bounds a download by default.
const artifactMaxBytes = 128 << 20

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

// artifactOptions caches artifacts in the user cache directory and pulls
// oci:// sources.
func artifactOptions() pluginartifact.Options {
	base, err := os.UserCacheDir()
	if err != nil {
		return pluginartifact.Options{PullOCI: oci.Pull}
	}
	return pluginartifact.Options{CacheDir: filepath.Join(base, "actionlint", "artifacts"), PullOCI: oci.Pull}
}

// PinPluginSource returns source with the tag of an
// oci://<host>/<repository>:<tag> source replaced by the digest the tag
// names now. Other sources are returned unchanged.
func PinPluginSource(ctx context.Context, source string) (string, error) {
	if !pluginartifact.IsOCI(source) {
		return source, nil
	}
	return oci.Pin(ctx, source, artifactOptions())
}

// InspectPlugin resolves and reads metadata without creating a runtime.
func InspectPlugin(ctx context.Context, source string) (pluginartifact.Info, error) {
	artifact, err := pluginartifact.Resolve(ctx, source, artifactOptions())
	if err != nil {
		return pluginartifact.Info{}, err
	}
	return artifact.Info, nil
}

// metadataTools returns the engine name and rule names that static plugin
// metadata declares. It reads the inspected metadata without running the
// plugin.
func metadataTools(metadata json.RawMessage) ([]string, error) {
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

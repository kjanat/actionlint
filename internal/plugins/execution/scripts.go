// Package execution loads plugin implementations for the requested extension role.
package execution

import (
	"context"
	"errors"
	"fmt"
	"io"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

// ScriptEngineLoader loads configured core modules and script-engine components
// with the same artifact verification and lifecycle for every native entrypoint.
func ScriptEngineLoader(acquisition plugininstall.Acquisition, reserved rule.Names, log io.Writer, debug bool) func(context.Context, string) (checkinputs.ScriptBackend, error) {
	return func(ctx context.Context, path string) (checkinputs.ScriptBackend, error) {
		if err := pluginartifact.RequireRemotePin(path); err != nil {
			return nil, err
		}
		artifact, err := pluginartifact.Resolve(ctx, path, acquisition.ArtifactOptions())
		if err != nil {
			return nil, err
		}
		return LoadScriptArtifact(ctx, artifact, acquisition, reserved, log, debug)
	}
}

// LoadScriptArtifact opens a verified core module or component that implements script checking.
func LoadScriptArtifact(ctx context.Context, artifact *pluginartifact.Artifact, acquisition plugininstall.Acquisition, reserved rule.Names, log io.Writer, debug bool) (checkinputs.ScriptBackend, error) {
	if artifact.Info.Kind != pluginartifact.Core && artifact.Info.Kind != pluginartifact.Component {
		return nil, fmt.Errorf("%s is a %s artifact, not a WebAssembly module", artifact.Info.Source, artifact.Info.Kind)
	}
	if artifact.Info.Kind == pluginartifact.Component {
		options := component.Options{ReservedRules: reserved}
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
	engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: acquisition.CompiledModuleCacheDir()})
	if err != nil {
		return nil, err
	}
	if debug {
		metadata := engine.Metadata()
		_, _ = fmt.Fprintf(log, "[Linter] Loaded engine %q (%s %s) from %s (%s)\n", metadata.ID, metadata.Engine.Name, metadata.Engine.Version, engine.Path(), engine.Limits())
	}
	return engine, nil
}

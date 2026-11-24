// Package pluginexecution loads plugin implementations for the requested extension role.
package pluginexecution

import (
	"context"
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
)

func ScriptEngineLoader(acquisition dependencycache.Acquisition, reserved rule.Names, log io.Writer, debug bool) func(context.Context, string) (scriptengine.Backend, error) {
	return func(ctx context.Context, path string) (scriptengine.Backend, error) {
		if err := artifacts.RequireRemotePin(path); err != nil {
			return nil, err
		}
		artifact, err := artifacts.Resolve(ctx, path, pluginacquisition.ArtifactOptions(acquisition))
		if err != nil {
			return nil, err
		}
		return LoadScriptArtifact(ctx, artifact, acquisition, reserved, log, debug)
	}
}

func LoadScriptArtifact(ctx context.Context, artifact *artifacts.Artifact, acquisition dependencycache.Acquisition, reserved rule.Names, log io.Writer, debug bool) (scriptengine.Backend, error) {
	if artifact.Info.Kind != artifacts.Core && artifact.Info.Kind != artifacts.Component {
		return nil, fmt.Errorf("%s is a %s artifact, not a WebAssembly module", artifact.Info.Source, artifact.Info.Kind)
	}
	if artifact.Info.Kind == artifacts.Component {
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
	engine, err := module.LoadArtifact(ctx, artifact, module.Options{CacheDir: pluginacquisition.CompiledModuleCacheDir(acquisition)})
	if err != nil {
		return nil, err
	}
	if debug {
		metadata := engine.Metadata()
		_, _ = fmt.Fprintf(log, "[Linter] Loaded engine %q (%s %s) from %s (%s)\n", metadata.ID, metadata.Engine.Name, metadata.Engine.Version, engine.Path(), engine.Limits())
	}
	return engine, nil
}

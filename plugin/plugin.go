// Package plugin loads WebAssembly plugins: components that implement
// kjanat:actionlint/analyzer, runner-provider or script-engine, and core modules that
// speak the wasm-core-json ABI.
package plugin

import (
	"context"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
)

// Analyzer is a compatibility alias for Component.
type Analyzer = Component

// Load loads a component from a path, file URI or SHA256-pinned HTTP(S) URL.
func Load(ctx context.Context, path string, options Options) (*Component, error) {
	if err := pluginartifact.RequireRemotePin(path); err != nil {
		return nil, err
	}
	grant, err := pluginartifact.ExplicitSource(path)
	if err != nil {
		return nil, err
	}
	internal := options.internal()
	internal.Artifact.Network = grant
	loaded, err := component.Load(ctx, path, internal)
	if err != nil {
		return nil, err
	}
	return &Component{loaded: loaded}, nil
}

// LoadEngine loads a core script engine from a raw or gzip-compressed module
// path, file URI or HTTP(S) URL. Set the result as analysis.Tool.Backend.
func LoadEngine(ctx context.Context, path string, options EngineOptions) (*Engine, error) {
	if err := pluginartifact.RequireRemotePin(path); err != nil {
		return nil, err
	}
	grant, err := pluginartifact.ExplicitSource(path)
	if err != nil {
		return nil, err
	}
	internal := options.internal()
	internal.Artifact.Network = grant
	loaded, err := corewasm.Load(ctx, path, internal)
	if err != nil {
		return nil, err
	}
	return &Engine{loaded: loaded}, nil
}

// Inspect resolves a plugin and reads its actionlint.plugin custom section.
// It never compiles, instantiates or executes WebAssembly code.
func Inspect(ctx context.Context, source string, options ArtifactOptions) (ArtifactInfo, error) {
	grant, err := pluginartifact.ExplicitSource(source)
	if err != nil {
		return ArtifactInfo{}, err
	}
	internal := artifactOptionsToInternal(options)
	internal.Network = grant
	artifact, err := pluginartifact.Resolve(ctx, source, internal)
	if err != nil {
		return ArtifactInfo{}, err
	}
	return artifactInfoFromInternal(artifact.Info), nil
}

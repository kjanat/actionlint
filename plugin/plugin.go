// Package plugin loads WebAssembly plugins: components that implement
// kjanat:actionlint/analyzer or runner-provider and core script engines that follow the
// actionlint-plugin-ruff JSON ABI.
package plugin

import (
	"context"

	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
	"actionlint.kjanat.dev/internal/plugins/component"
	corewasm "actionlint.kjanat.dev/internal/plugins/core"
)

// Component is a loaded plugin. Inspect HasAnalyzer and HasRunnerProvider before
// registering it for either role. A component may implement both.
type Component = component.Plugin

// Analyzer is the original name for Component, retained for compatibility.
type Analyzer = component.Plugin

// Options configures a component.
type Options = component.Options

// Limits reports the bounds an analyzer or engine runs under.
type Limits = component.Limits

// Load loads a component from a path, file URI or HTTP(S) URL.
func Load(ctx context.Context, path string, options Options) (*Component, error) {
	return component.Load(ctx, path, options)
}

// Engine is a loaded core module. It implements analyzer.ScriptBackend and
// analyzer.ScriptConfigValidator.
type Engine = corewasm.Engine

// EngineOptions configures a core script engine.
type EngineOptions = corewasm.Options

// LoadEngine loads a core script engine from a raw or gzip-compressed module
// path, file URI or HTTP(S) URL. Set the result as analysis.Tool.Backend.
func LoadEngine(ctx context.Context, path string, options EngineOptions) (*Engine, error) {
	return corewasm.Load(ctx, path, options)
}

// ArtifactOptions configures bounded acquisition and pinned download caching.
type ArtifactOptions = pluginartifact.Options

// ArtifactInfo describes a binary and its untrusted static custom metadata.
type ArtifactInfo = pluginartifact.Info

// ArtifactKind identifies the binary format independently of its metadata.
type ArtifactKind = pluginartifact.Kind

const (
	CoreArtifact      ArtifactKind = pluginartifact.Core
	ComponentArtifact ArtifactKind = pluginartifact.Component
)

// Inspect resolves a plugin and reads its actionlint.plugin custom section.
// It never compiles, instantiates or executes WebAssembly code.
func Inspect(ctx context.Context, source string, options ArtifactOptions) (ArtifactInfo, error) {
	artifact, err := pluginartifact.Resolve(ctx, source, options)
	if err != nil {
		return ArtifactInfo{}, err
	}
	return artifact.Info, nil
}

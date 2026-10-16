// Package plugin loads WebAssembly plugins: component analyzers that implement
// kjanat:actionlint/analyzer and core script engines that follow the
// actionlint-plugin-ruff JSON ABI.
package plugin

import (
	"context"

	"actionlint.kjanat.dev/internal/component"
	"actionlint.kjanat.dev/internal/corewasm"
	"actionlint.kjanat.dev/internal/pluginartifact"
)

// Analyzer is a loaded component. It implements analyzer.Analyzer.
type Analyzer = component.Plugin

// Options configures a component analyzer.
type Options = component.Options

// Limits reports the bounds an analyzer or engine runs under.
type Limits = component.Limits

// Load loads a component analyzer from a path, file URI or HTTP(S) URL.
func Load(ctx context.Context, path string, options Options) (*Analyzer, error) {
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

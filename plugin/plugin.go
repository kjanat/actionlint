// Package plugin loads WebAssembly plugins: component analyzers that implement
// kjanat:actionlint/analyzer and core script engines that follow the
// actionlint-plugin-ruff JSON ABI.
package plugin

import (
	"context"

	"actionlint.kjanat.dev/internal/component"
	"actionlint.kjanat.dev/internal/corewasm"
)

// Analyzer is a loaded component. It implements analyzer.Analyzer.
type Analyzer = component.Plugin

// Options configures a component analyzer.
type Options = component.Options

// Limits reports the bounds a component analyzer runs under.
type Limits = component.Limits

// EngineLimits reports the bounds a core script engine runs under.
type EngineLimits = corewasm.Limits

// Load loads a component analyzer from a file.
func Load(ctx context.Context, path string, options Options) (*Analyzer, error) {
	return component.Load(ctx, path, options)
}

// Engine is a loaded core module. It implements analyzer.ScriptBackend and
// analyzer.ScriptConfigValidator.
type Engine = corewasm.Engine

// EngineOptions configures a core script engine.
type EngineOptions = corewasm.Options

// LoadEngine loads a core script engine from a raw or gzip-compressed module
// file. Set the result as analysis.Tool.Backend.
func LoadEngine(ctx context.Context, path string, options EngineOptions) (*Engine, error) {
	return corewasm.Load(ctx, path, options)
}

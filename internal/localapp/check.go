// Package localapp supplies the CLI application with local WebAssembly runtimes.
// Library callers can use application directly and inject their own backend loader.
package localapp

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/internal/processconfig"

	"actionlint.kjanat.dev/internal/application"
)

// PluginLoadError identifies invalid plugin selection without imposing CLI status codes.
type PluginLoadError struct{ Err error }

func (e *PluginLoadError) Error() string { return e.Err.Error() }
func (e *PluginLoadError) Unwrap() error { return e.Err }

// Check loads selected plugins, runs analysis and releases plugin resources.
func Check(ctx context.Context, options application.Options, input application.Input, paths []string) (result *application.Result, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	plugins, err := loadPlugins(ctx, paths, log, options.Debug)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	if len(plugins.engines) > 0 {
		if options.RuffBackend != nil || options.RuffOptions != nil && !reflect.DeepEqual(*options.RuffOptions, processconfig.Options{Optional: true}) || (options.Ruff != "" && options.Ruff != "ruff") {
			return nil, &PluginLoadError{errors.New("ruff plugin conflicts with an explicitly selected Ruff backend or command")}
		}
		options.Ruff = ""
		options.RuffOptions = nil
		options.RuffBackend = plugins.engines[0]
	}
	options.Analyzers = slices.Concat(options.Analyzers, plugins.registrations())
	if options.LoadScriptEngine == nil {
		options.LoadScriptEngine = scriptEngineLoader(log, options.Debug)
	}
	return application.Check(ctx, options, input)
}

// Package localapp supplies the CLI application with local WebAssembly runtimes.
// Library callers can use application directly and inject their own backend loader.
package localapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"actionlint.kjanat.dev/internal/tools/backend"

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
	for _, engine := range plugins.engines {
		metadata := engine.Metadata()
		found := false
		options.Tools = slices.Clone(options.Tools)
		for i, registration := range options.Tools {
			if registration.Descriptor.Name != metadata.Engine.Name {
				continue
			}
			if registration.WithBackend == nil {
				return nil, &PluginLoadError{fmt.Errorf("tool %q cannot replace its backend", metadata.Engine.Name)}
			}
			replacement, err := registration.WithBackend(engine)
			if err != nil {
				return nil, &PluginLoadError{err}
			}
			options.Tools[i] = replacement
			found = true
			break
		}
		if !found {
			descriptor := engineDescriptor(metadata)
			registration, err := backend.Register(descriptor, metadata.Capabilities.ScriptShells, engine)
			if err != nil {
				return nil, &PluginLoadError{err}
			}
			options.Tools = append(options.Tools, registration)
		}
	}

	options.Analyzers = slices.Concat(options.Analyzers, plugins.registrations())
	if options.LoadScriptEngine == nil {
		options.LoadScriptEngine = scriptEngineLoader(log, options.Debug)
	}
	return application.Check(ctx, options, input)
}

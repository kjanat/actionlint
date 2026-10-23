// Package localapp supplies the CLI application with local WebAssembly runtimes.
// Library callers can use application directly and inject their own backend loader.
package localapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/tools/backend"

	"actionlint.kjanat.dev/v2/internal/application"
)

// PluginLoadError identifies invalid plugin selection without imposing CLI status codes.
type PluginLoadError struct{ Err error }

func (e *PluginLoadError) Error() string { return e.Err.Error() }
func (e *PluginLoadError) Unwrap() error { return e.Err }

// Check loads selected plugins, runs analysis and releases plugin resources.
func Check(ctx context.Context, options application.Options, input application.Input, paths []string) (result *application.Result, err error) {
	return CheckSources(ctx, options, input, PluginSources{Paths: paths})
}

// CheckSources preserves declared registry identities when choosing managed
// tool replacements. Direct URLs and explicit files remain standalone analyzers.
func CheckSources(ctx context.Context, options application.Options, input application.Input, sources PluginSources) (result *application.Result, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	if sources.lock != nil {
		if err := sources.lock.CheckStatuses(ctx, pluginregistry.StatusOptions{Warning: func(message string) { _, _ = fmt.Fprintln(log, "warning:", message) }}); err != nil {
			return nil, &PluginLoadError{err}
		}
	}
	plugins, err := loadPlugins(ctx, sources, log, options.Debug)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, plugins.close(context.WithoutCancel(ctx))) }()
	options, err = plugins.configure(options)
	if err != nil {
		return nil, err
	}
	options, err = plugins.configureManaged(options, sources.managed)
	if err != nil {
		return nil, err
	}
	if options.LoadScriptEngine == nil {
		options.LoadScriptEngine = ScriptEngineLoader(log, options.Debug)
	}
	return application.Check(ctx, options, input)
}

// RequiredTools applies plugin replacements before deciding which native tools
// must be provisioned. It shares plugin validation and lifetime with Check.
func RequiredTools(ctx context.Context, options application.Options, paths, plugins []string) (needed application.ToolRequirements, err error) {
	return RequiredToolsSources(ctx, options, paths, PluginSources{Paths: plugins})
}

// RequiredToolsSources retains declared replacement roles during native-tool
// discovery, using the same configuration policy as CheckSources.
func RequiredToolsSources(ctx context.Context, options application.Options, paths []string, sources PluginSources) (needed application.ToolRequirements, err error) {
	loaded, err := loadPlugins(ctx, sources, io.Discard, false)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, loaded.close(context.WithoutCancel(ctx))) }()
	options, err = loaded.configure(options)
	if err != nil {
		return nil, err
	}
	options, err = loaded.configureManaged(options, sources.managed)
	if err != nil {
		return nil, err
	}
	return application.RequiredTools(ctx, options, paths)
}

func (l *loadedPlugins) configureManaged(options application.Options, managed map[string]string) (application.Options, error) {
	options.Tools = slices.Clone(options.Tools)
	for path, tool := range managed {
		guest := l.analyzerSources[path]
		if guest == nil {
			return options, &PluginLoadError{fmt.Errorf("managed %s plugin does not export a document analyzer", tool)}
		}
		found := false
		for i, registration := range options.Tools {
			if registration.Descriptor.Name != tool {
				continue
			}
			if registration.WithAnalyzer == nil {
				return options, &PluginLoadError{fmt.Errorf("tool %q cannot use a document analyzer", tool)}
			}
			next, err := registration.WithAnalyzer(guest)
			if err != nil {
				return options, &PluginLoadError{err}
			}
			options.Tools[i], found = backend.Configure(next), true
			break
		}
		if !found {
			return options, &PluginLoadError{fmt.Errorf("managed plugin requires tool %q", tool)}
		}
		options.Analyzers = slices.DeleteFunc(slices.Clone(options.Analyzers), func(registration analyzer.Registration) bool {
			return registration.Analyzer == guest
		})
	}
	return options, nil
}

func (l *loadedPlugins) configure(options application.Options) (application.Options, error) {
	seen := map[string]bool{}
	for _, engine := range l.engines {
		metadata := engine.descriptor
		if seen[metadata.Engine] {
			return options, &PluginLoadError{fmt.Errorf("multiple plugins provide script engine %q", metadata.Engine)}
		}
		seen[metadata.Engine] = true
		found := false
		options.Tools = slices.Clone(options.Tools)
		for i, registration := range options.Tools {
			if registration.Descriptor.Name != metadata.Engine {
				continue
			}
			if registration.WithBackend == nil {
				return options, &PluginLoadError{fmt.Errorf("tool %q cannot replace its backend", metadata.Engine)}
			}
			replacement, err := registration.WithBackend(engine.backend)
			if err != nil {
				return options, &PluginLoadError{err}
			}
			replacement, err = backend.WithScriptInputs(replacement, metadata, engine.backend)
			if err != nil {
				return options, &PluginLoadError{err}
			}
			options.Tools[i] = replacement
			found = true
			break
		}
		if !found {
			registration, err := backend.RegisterEngine(metadata, engine.backend)
			if err != nil {
				return options, &PluginLoadError{err}
			}
			options.Tools = append(options.Tools, registration)
		}
	}

	options.Tools = slices.Clone(options.Tools)
	for i, registration := range options.Tools {
		options.Tools[i] = backend.Configure(registration)
	}
	options.Analyzers = slices.Concat(options.Analyzers, l.registrations())
	options.Runners.Providers = slices.Concat(options.Runners.Providers, l.runnerProviders())
	return options, nil
}

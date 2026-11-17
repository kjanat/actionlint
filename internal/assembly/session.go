package assembly

import (
	"context"
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

// Session holds checking options with the selected plugins loaded and
// registered. The command line, the language servers and the tool queries
// all build their checks through Open. Close releases the plugins.
type Session struct {
	// Options are the caller's options with the plugins' reference resolver,
	// script engines, tool providers, analyzers and runner providers
	// registered in them.
	Options checking.Options
	plugins *pluginexecution.Loaded
}

// Open checks the cached release status of the selected plugins, loads them
// and registers them in options. It writes plugin warnings and, with
// options.Debug, plugin logs to options.LogWriter. A failed Open releases
// what it loaded.
func Open(ctx context.Context, options checking.Options, sources pluginselection.Sources) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	if err := CheckPluginStatuses(ctx, sources, log); err != nil {
		return nil, err
	}
	plugins, err := pluginexecution.LoadPlugins(ctx, sources, checking.BuiltinRuleNames(), log, options.Debug)
	if err != nil {
		return nil, err
	}
	options, err = ConfigurePlugins(plugins, ctx, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	options, err = ConfigureManagedPlugins(plugins, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	return &Session{Options: options, plugins: plugins}, nil
}

// BeginAnalysis prepares the loaded plugins for another analysis in a
// long-lived session.
func (s *Session) BeginAnalysis(ctx context.Context) error { return s.plugins.BeginAnalysis(ctx) }

// Close releases the loaded plugins.
func (s *Session) Close(ctx context.Context) error {
	return s.plugins.Close(context.WithoutCancel(ctx))
}

// CheckPluginStatuses rejects selected plugins that their registry's cached
// release status revokes, and writes status warnings to log. It reads no
// network.
func CheckPluginStatuses(ctx context.Context, sources pluginselection.Sources, log io.Writer) error {
	if sources.Releases == nil {
		return nil
	}
	if log == nil {
		log = io.Discard
	}
	if err := sources.Releases.CheckStatuses(ctx, pluginregistry.StatusOptions{Warning: func(message string) {
		_, _ = fmt.Fprintln(log, "warning:", message)
	}, Acquisition: sources.Acquisition}); err != nil {
		return &pluginexecution.LoadError{Err: err}
	}
	return nil
}

// loadDeclarations loads the selected plugins to read the rules they declare,
// with the bundled tool providers registered. It runs no tool provider and
// downloads no executable. The caller closes the returned plugins.
func loadDeclarations(ctx context.Context, sources pluginselection.Sources) (*pluginexecution.Loaded, checking.Options, error) {
	plugins, err := pluginexecution.LoadPlugins(ctx, sources, checking.BuiltinRuleNames(), io.Discard, false)
	if err != nil {
		return nil, checking.Options{}, err
	}
	configured, err := ConfigureManagedPlugins(plugins, checking.Options{Tools: registry.Registrations(nil), Analyzers: plugins.Registrations()})
	if err != nil {
		return nil, checking.Options{}, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	return plugins, configured, nil
}

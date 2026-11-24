package assembly

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

type Session struct {
	Options checking.Options
	plugins *pluginexecution.Loaded
	tools   []contract.Registration
	mu      sync.Mutex
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
	tools := slices.Clone(options.Tools)
	options, err = ConfigurePlugins(plugins, ctx, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	options, err = ConfigureManagedPlugins(plugins, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	return &Session{Options: options, plugins: plugins, tools: tools}, nil
}

// BeginAnalysis prepares the loaded plugins for another analysis in a
// long-lived session and returns the options for that analysis.
func (s *Session) BeginAnalysis(ctx context.Context) (checking.Options, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed, err := s.plugins.BeginAnalysis(ctx)
	for _, engine := range changed {
		tools, refreshErr := refreshScriptEngine(s.Options.Tools, s.tools, engine)
		if refreshErr != nil {
			return s.Options, errors.Join(err, &pluginexecution.LoadError{Err: refreshErr})
		}
		s.Options.Tools = tools
	}
	return s.Options, err
}

func (s *Session) Close(ctx context.Context) error {
	return s.plugins.Close(context.WithoutCancel(ctx))
}

// CheckPluginStatuses rejects selected plugins whose cached release status in
// their registry is revoked, and writes status warnings to log. It reads no
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

// loadDeclarations loads the selected plugins to read their declared rules and
// registers the bundled tool providers. It runs no tool provider and
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

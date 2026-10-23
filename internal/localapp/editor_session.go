package localapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

// An editor session owns compiled plugins, never document or analysis state.
// Explicit CLI plugins and remote URLs are snapshots until server restart.
// Configured local artifacts are identified by content; changing their bytes or
// selecting a different URI loads a new engine. Old engines remain owned until
// shutdown so analyses already using them can finish safely.
type editorSession struct {
	options application.Options
	plugins *loadedPlugins
	engines *editorEngines
	sources PluginSources
}

func newEditorSession(ctx context.Context, options application.Options, paths []string) (*editorSession, error) {
	return newEditorSources(ctx, options, PluginSources{Paths: paths})
}

func newEditorSources(ctx context.Context, options application.Options, sources PluginSources) (*editorSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	if err := checkEditorPluginStatuses(ctx, sources, log); err != nil {
		return nil, err
	}
	plugins, err := loadPlugins(ctx, sources, log, options.Debug)
	if err != nil {
		return nil, err
	}
	sources.artifacts = nil
	options, err = plugins.configure(options)
	if err != nil {
		return nil, errors.Join(err, plugins.close(context.WithoutCancel(ctx)))
	}
	options, err = plugins.configureManaged(options, sources.managed)
	if err != nil {
		return nil, errors.Join(err, plugins.close(context.WithoutCancel(ctx)))
	}
	engines := &editorEngines{
		gate: make(chan struct{}, 1), loaded: make(map[editorEngineIdentity]analyzer.ScriptBackend),
		customLoad: options.LoadScriptEngine, log: log, debug: options.Debug,
	}
	options.LoadScriptEngine = engines.load
	return &editorSession{options: options, plugins: plugins, engines: engines, sources: sources}, nil
}

func checkEditorPluginStatuses(ctx context.Context, sources PluginSources, log io.Writer) error {
	if sources.lock == nil {
		return nil
	}
	if log == nil {
		log = io.Discard
	}
	if err := sources.lock.CheckStatuses(ctx, pluginregistry.StatusOptions{Warning: func(message string) {
		_, _ = fmt.Fprintln(log, "warning:", message)
	}}); err != nil {
		return &PluginLoadError{err}
	}
	return nil
}

// Serve waits for every analysis before this releases their shared resources.
func (s *editorSession) close(ctx context.Context) error {
	return errors.Join(s.plugins.close(ctx), s.engines.close(ctx))
}

type editorEngineIdentity struct {
	source string
	digest string
}

type editorEngines struct {
	gate       chan struct{}
	loaded     map[editorEngineIdentity]analyzer.ScriptBackend
	customLoad func(context.Context, string) (analyzer.ScriptBackend, error)
	log        io.Writer
	debug      bool
}

func (s *editorEngines) load(ctx context.Context, source string) (analyzer.ScriptBackend, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity := editorEngineIdentity{source: source}
	// An injected loader owns source interpretation. Its source string is the
	// session identity; remote artifacts likewise remain a stable snapshot.
	if backend, ok := s.loaded[identity]; ok {
		return borrowEditorEngine(backend), nil
	}
	var backend analyzer.ScriptBackend
	var err error
	if s.customLoad != nil {
		backend, err = s.customLoad(ctx, source)
	} else {
		if err := pluginartifact.RequireRemotePin(source); err != nil {
			return nil, err
		}
		artifact, resolveErr := pluginartifact.Resolve(ctx, source, artifactOptions())
		if resolveErr != nil {
			return nil, resolveErr
		}
		remote := strings.HasPrefix(strings.ToLower(source), "http://") || strings.HasPrefix(strings.ToLower(source), "https://")
		if !remote {
			identity.digest = artifact.Info.SHA256
		}
		if existing, ok := s.loaded[identity]; ok {
			return borrowEditorEngine(existing), nil
		}
		engine, loadErr := loadScriptArtifact(ctx, artifact, s.log, s.debug)
		if loadErr != nil {
			return nil, loadErr
		}
		backend = engine
	}
	if err != nil {
		// A superseded analysis must not poison the next generation's load.
		return nil, err
	}
	s.loaded[identity] = backend
	return borrowEditorEngine(backend), nil
}

func (s *editorEngines) close(ctx context.Context) error {
	var err error
	for identity, backend := range s.loaded {
		if closer, ok := backend.(interface{ Close(context.Context) error }); ok {
			err = errors.Join(err, closer.Close(ctx))
		}
		delete(s.loaded, identity)
	}
	return err
}

// application.Check owns per-analysis loaders. A borrowed backend deliberately
// exposes no Close method while retaining validation and selector capabilities.
type (
	borrowedEditorEngine  struct{ analyzer.ScriptBackend }
	describedEditorEngine struct {
		analyzer.DescribedScriptBackend
	}
	validatingDescribedEditorEngine struct {
		analyzer.DescribedScriptBackend
		analyzer.ScriptConfigValidator
	}
	validatingEditorEngine struct {
		analyzer.ScriptBackend
		analyzer.ScriptConfigValidator
	}
)

func borrowEditorEngine(backend analyzer.ScriptBackend) analyzer.ScriptBackend {
	if described, ok := backend.(analyzer.DescribedScriptBackend); ok {
		if validator, ok := backend.(analyzer.ScriptConfigValidator); ok {
			return validatingDescribedEditorEngine{DescribedScriptBackend: described, ScriptConfigValidator: validator}
		}
		return describedEditorEngine{DescribedScriptBackend: described}
	}
	if validator, ok := backend.(analyzer.ScriptConfigValidator); ok {
		return validatingEditorEngine{ScriptBackend: backend, ScriptConfigValidator: validator}
	}
	return borrowedEditorEngine{ScriptBackend: backend}
}

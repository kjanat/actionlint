package localapp

import (
	"context"
	"errors"
	"io"
	"strings"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
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
}

func newEditorSession(ctx context.Context, options application.Options, paths []string) (*editorSession, error) {
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
	options, err = plugins.configure(options)
	if err != nil {
		return nil, errors.Join(err, plugins.close(context.WithoutCancel(ctx)))
	}
	engines := &editorEngines{
		gate: make(chan struct{}, 1), loaded: make(map[editorEngineIdentity]analyzer.ScriptBackend),
		customLoad: options.LoadScriptEngine, log: log, debug: options.Debug,
	}
	options.LoadScriptEngine = engines.load
	return &editorSession{options: options, plugins: plugins, engines: engines}, nil
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

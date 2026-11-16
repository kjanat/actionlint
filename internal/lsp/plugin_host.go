package lsp

import (
	"context"
	"errors"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"actionlint.kjanat.dev/v2/internal/assembly"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// An editor session owns compiled plugins, never document or analysis state.
// Explicit CLI plugins and remote URLs are snapshots until server restart.
// Configured local artifacts are identified by content; changing their bytes or
// selecting a different URI loads a new engine. Engines are closed at shutdown
// because in-flight analyses may use an earlier version.
type editorPluginHost struct {
	options checking.Options
	session *assembly.Session
	engines *editorEngines
	sources pluginselection.Sources
}

func newEditorPluginHost(ctx context.Context, options checking.Options, paths []string, acquisition dependencycache.Acquisition) (*editorPluginHost, error) {
	sources, err := pluginselection.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return newEditorSources(ctx, options, sources)
}

func newEditorSources(ctx context.Context, options checking.Options, sources pluginselection.Sources) (*editorPluginHost, error) {
	session, err := assembly.Open(ctx, options, sources)
	if err != nil {
		return nil, err
	}
	sources.Artifacts = nil
	options = session.Options
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	engines := &editorEngines{
		gate: make(chan struct{}, 1), loaded: make(map[editorEngineIdentity]scriptengine.Backend),
		customLoad: options.LoadScriptEngine, log: log, debug: options.Debug, acquisition: sources.Acquisition,
	}
	options.LoadScriptEngine = engines.load
	return &editorPluginHost{options: options, session: session, engines: engines, sources: sources}, nil
}

// Serve waits for every analysis before this releases their shared resources.
func (s *editorPluginHost) close(ctx context.Context) error {
	return errors.Join(s.session.Close(ctx), s.engines.close(ctx))
}

type editorEngineIdentity struct {
	source string
	digest string
}

type editorEngines struct {
	gate        chan struct{}
	loaded      map[editorEngineIdentity]scriptengine.Backend
	customLoad  func(context.Context, string) (scriptengine.Backend, error)
	log         io.Writer
	debug       bool
	acquisition dependencycache.Acquisition
}

func (s *editorEngines) load(ctx context.Context, source string) (scriptengine.Backend, error) {
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
	// Cache injected and remote engines by their source string.
	if backend, ok := s.loaded[identity]; ok {
		return borrowEditorEngine(backend), nil
	}
	var backend scriptengine.Backend
	var err error
	if s.customLoad != nil {
		backend, err = s.customLoad(ctx, source)
	} else {
		if err := artifacts.RequireRemotePin(source); err != nil {
			return nil, err
		}
		artifact, resolveErr := artifacts.Resolve(ctx, source, pluginacquisition.ArtifactOptions(s.acquisition))
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
		engine, loadErr := pluginexecution.LoadScriptArtifact(ctx, artifact, s.acquisition, checking.BuiltinRuleNames(), s.log, s.debug)
		if loadErr != nil {
			return nil, loadErr
		}
		backend = engine
	}
	if err != nil {
		// Do not cache a failed load, so a later analysis can retry it.
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

// Hide Close so per-analysis cleanup cannot close a session-owned engine.
type (
	borrowedEditorEngine  struct{ scriptengine.Backend }
	describedEditorEngine struct {
		scriptengine.DescribedBackend
	}
	validatingDescribedEditorEngine struct {
		scriptengine.DescribedBackend
		scriptengine.ConfigValidator
	}
	validatingEditorEngine struct {
		scriptengine.Backend
		scriptengine.ConfigValidator
	}
)

func borrowEditorEngine(backend scriptengine.Backend) scriptengine.Backend {
	if described, ok := backend.(scriptengine.DescribedBackend); ok {
		if validator, ok := backend.(scriptengine.ConfigValidator); ok {
			return validatingDescribedEditorEngine{DescribedBackend: described, ConfigValidator: validator}
		}
		return describedEditorEngine{DescribedBackend: described}
	}
	if validator, ok := backend.(scriptengine.ConfigValidator); ok {
		return validatingEditorEngine{Backend: backend, ConfigValidator: validator}
	}
	return borrowedEditorEngine{Backend: backend}
}

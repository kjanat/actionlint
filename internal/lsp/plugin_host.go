package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// An editor session owns compiled plugins, never document or analysis state.
// Explicit CLI plugins and remote URLs are snapshots until server restart.
// Configured local artifacts are identified by content; changing their bytes or
// selecting a different URI loads a new engine. Engines are closed at shutdown
// because in-flight analyses may use an earlier version.
type editorPluginHost struct {
	options checking.Options
	plugins *pluginexec.Loaded
	engines *editorEngines
	sources pluginselection.Sources
}

func newEditorPluginHost(ctx context.Context, options checking.Options, paths []string, acquisition pluginacquisition.Settings) (*editorPluginHost, error) {
	sources, err := pluginselection.SourcesFromArguments(paths, acquisition)
	if err != nil {
		return nil, err
	}
	return newEditorSources(ctx, options, sources)
}

func newEditorSources(ctx context.Context, options checking.Options, sources pluginselection.Sources) (*editorPluginHost, error) {
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
	plugins, err := pluginexec.LoadPlugins(ctx, sources, checking.BuiltinRuleNames(), log, options.Debug)
	if err != nil {
		return nil, err
	}
	sources.Artifacts = nil
	options, err = checking.ConfigurePlugins(plugins, ctx, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	options, err = checking.ConfigureManagedPlugins(plugins, options)
	if err != nil {
		return nil, errors.Join(err, plugins.Close(context.WithoutCancel(ctx)))
	}
	engines := &editorEngines{
		gate: make(chan struct{}, 1), loaded: make(map[editorEngineIdentity]inputs.ScriptBackend),
		customLoad: options.LoadScriptEngine, log: log, debug: options.Debug, acquisition: sources.Acquisition,
	}
	options.LoadScriptEngine = engines.load
	return &editorPluginHost{options: options, plugins: plugins, engines: engines, sources: sources}, nil
}

func checkEditorPluginStatuses(ctx context.Context, sources pluginselection.Sources, log io.Writer) error {
	if sources.Releases == nil {
		return nil
	}
	if log == nil {
		log = io.Discard
	}
	if err := sources.Releases.CheckStatuses(ctx, pluginregistry.StatusOptions{Warning: func(message string) {
		_, _ = fmt.Fprintln(log, "warning:", message)
	}, Acquisition: sources.Acquisition}); err != nil {
		return &pluginexec.LoadError{Err: err}
	}
	return nil
}

// Serve waits for every analysis before this releases their shared resources.
func (s *editorPluginHost) close(ctx context.Context) error {
	return errors.Join(s.plugins.Close(ctx), s.engines.close(ctx))
}

type editorEngineIdentity struct {
	source string
	digest string
}

type editorEngines struct {
	gate        chan struct{}
	loaded      map[editorEngineIdentity]inputs.ScriptBackend
	customLoad  func(context.Context, string) (inputs.ScriptBackend, error)
	log         io.Writer
	debug       bool
	acquisition pluginacquisition.Settings
}

func (s *editorEngines) load(ctx context.Context, source string) (inputs.ScriptBackend, error) {
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
	var backend inputs.ScriptBackend
	var err error
	if s.customLoad != nil {
		backend, err = s.customLoad(ctx, source)
	} else {
		if err := artifacts.RequireRemotePin(source); err != nil {
			return nil, err
		}
		artifact, resolveErr := artifacts.Resolve(ctx, source, s.acquisition.ArtifactOptions())
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
		engine, loadErr := pluginexec.LoadScriptArtifact(ctx, artifact, s.acquisition, checking.BuiltinRuleNames(), s.log, s.debug)
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
	borrowedEditorEngine  struct{ inputs.ScriptBackend }
	describedEditorEngine struct {
		inputs.DescribedScriptBackend
	}
	validatingDescribedEditorEngine struct {
		inputs.DescribedScriptBackend
		inputs.ScriptConfigValidator
	}
	validatingEditorEngine struct {
		inputs.ScriptBackend
		inputs.ScriptConfigValidator
	}
)

func borrowEditorEngine(backend inputs.ScriptBackend) inputs.ScriptBackend {
	if described, ok := backend.(inputs.DescribedScriptBackend); ok {
		if validator, ok := backend.(inputs.ScriptConfigValidator); ok {
			return validatingDescribedEditorEngine{DescribedScriptBackend: described, ScriptConfigValidator: validator}
		}
		return describedEditorEngine{DescribedScriptBackend: described}
	}
	if validator, ok := backend.(inputs.ScriptConfigValidator); ok {
		return validatingEditorEngine{ScriptBackend: backend, ScriptConfigValidator: validator}
	}
	return borrowedEditorEngine{ScriptBackend: backend}
}

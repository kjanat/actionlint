package lsp

import (
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

// LanguageServer shares application selection, configuration and plugin loading
// with ordinary checks. Unsaved open buffers overlay dependency reads.
func LanguageServer(ctx context.Context, in io.Reader, out io.Writer, options checking.Options, plugins plugininstall.Sources) (err error) {
	directory, err := filepath.Abs(options.WorkingDir)
	if err != nil {
		return err
	}
	options.WorkingDir = directory
	session, err := newEditorSources(ctx, options, plugins)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.close(context.WithoutCancel(ctx))) }()
	return Serve(ctx, in, out, session.analyze, CompleteDocument)
}

func (s *editorSession) analyze(ctx context.Context, documents map[string]Document) (Analysis, error) {
	// A registry refresh in another process can revoke an already compiled
	// plugin. Recheck local status before every analysis without network access.
	if err := checkEditorPluginStatuses(ctx, s.sources, s.options.LogWriter); err != nil {
		return Analysis{}, err
	}
	if err := s.sources.ValidateScope(ctx, checking.ConfigurationRules(), slices.Collect(maps.Keys(documents))); err != nil {
		return Analysis{}, err
	}
	return analyzeDocuments(ctx, s.options, documents)
}

func analyzeDocuments(ctx context.Context, options checking.Options, documents map[string]Document) (Analysis, error) {
	return analyzeEditorDocuments(ctx, options, documents, func(ctx context.Context, options checking.Options, sources []workspace.Source) (*checking.Result, error) {
		paths := make([]string, 0, len(sources))
		for _, source := range sources {
			paths = append(paths, source.Path)
		}
		return checking.Check(ctx, options, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
	})
}

// AnalyzeEditorDocuments checks supplied editor buffers without filesystem
// discovery. ReadFile supplies closed dependencies; open buffers override it.
// Diagnostics and fixes use the same captured-source admission as the native LSP.
func AnalyzeEditorDocuments(ctx context.Context, options checking.Options, documents map[string]Document) (Analysis, error) {
	return analyzeEditorDocuments(ctx, options, documents, checking.Analyze)
}

func analyzeEditorDocuments(ctx context.Context, options checking.Options, documents map[string]Document, check func(context.Context, checking.Options, []workspace.Source) (*checking.Result, error)) (Analysis, error) {
	directory, err := filepath.Abs(options.WorkingDir)
	if err != nil {
		return Analysis{}, err
	}
	if len(documents) == 0 {
		return Analysis{}, nil
	}
	opts := options
	opts.WorkingDir = directory
	opts.SourceOverlays = slices.Clone(options.SourceOverlays)
	sources := make([]workspace.Source, 0, len(documents))
	project := workspace.Identity(directory)
	for _, path := range slices.Sorted(maps.Keys(documents)) {
		address := path
		if !filepath.IsAbs(address) {
			address = filepath.Join(directory, address)
		}
		sources = append(sources, workspace.Source{Path: address, Content: []byte(documents[path].Text), Project: project})
		opts.SourceOverlays = append(opts.SourceOverlays, checking.SourceOverlay{Path: address, Content: []byte(documents[path].Text)})
	}
	result, err := check(ctx, opts, sources)
	if ctx.Err() != nil {
		return Analysis{}, errors.Join(err, ctx.Err())
	}
	if result == nil {
		return Analysis{}, err
	}
	analysis, conversionErr := FromReport(result, directory, documents)
	if ctx.Err() != nil {
		return Analysis{}, errors.Join(err, conversionErr, ctx.Err())
	}
	return analysis, errors.Join(err, conversionErr)
}

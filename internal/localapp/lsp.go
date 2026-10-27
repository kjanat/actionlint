package localapp

import (
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/workflow/editor"
	"actionlint.kjanat.dev/v2/source"
)

// LanguageServer shares application selection, configuration and plugin loading
// with ordinary checks. Unsaved open buffers overlay dependency reads.
func LanguageServer(ctx context.Context, in io.Reader, out io.Writer, options application.Options, plugins PluginSources) (err error) {
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
	return langserver.Serve(ctx, in, out, session.analyze, func(ctx context.Context, doc langserver.Document, offset int) []langserver.Completion {
		if ctx.Err() != nil {
			return nil
		}
		items := editor.Complete(doc.Text, offset)
		result := make([]langserver.Completion, 0, len(items))
		for _, item := range items {
			result = append(result, langserver.Completion{Label: item.Label, Detail: item.Detail, Start: item.Start, End: item.End})
		}
		return result
	})
}

func (s *editorSession) analyze(ctx context.Context, documents map[string]langserver.Document) (langserver.Analysis, error) {
	// A registry refresh in another process can revoke an already compiled
	// plugin. Recheck local status before every analysis without network access.
	if err := checkEditorPluginStatuses(ctx, s.sources, s.options.LogWriter); err != nil {
		return langserver.Analysis{}, err
	}
	if err := s.sources.ValidateScope(slices.Collect(maps.Keys(documents))); err != nil {
		return langserver.Analysis{}, err
	}
	return analyzeDocuments(ctx, s.options, documents)
}

func analyzeDocuments(ctx context.Context, options application.Options, documents map[string]langserver.Document) (langserver.Analysis, error) {
	directory, err := filepath.Abs(options.WorkingDir)
	if err != nil {
		return langserver.Analysis{}, err
	}
	if len(documents) == 0 {
		return langserver.Analysis{}, nil
	}
	opts := options
	opts.WorkingDir = directory
	opts.SourceOverlays = slices.Clone(options.SourceOverlays)
	paths := make([]string, 0, len(documents))
	for _, path := range slices.Sorted(maps.Keys(documents)) {
		address := path
		if !filepath.IsAbs(address) {
			address = filepath.Join(directory, address)
		}
		paths = append(paths, address)
		opts.SourceOverlays = append(opts.SourceOverlays, application.SourceOverlay{Path: address, Content: []byte(documents[path].Text)})
	}
	result, err := application.Check(ctx, opts, application.Input{Kind: application.Files, Paths: paths})
	if result == nil {
		return langserver.Analysis{}, err
	}
	sources := make(map[string]string, len(result.CapturedSources))
	for path, captured := range result.CapturedSources {
		if captured.Coordinates == source.YAML && captured.CoordinatesAvailable {
			sources[path] = string(captured.Content)
		}
	}
	for i := range result.Diagnostics {
		d := &result.Diagnostics[i]
		if !filepath.IsAbs(d.Path) {
			d.Path = filepath.Join(options.WorkingDir, d.Path)
		}
		for j := range d.Related {
			if !filepath.IsAbs(d.Related[j].Path) {
				d.Related[j].Path = filepath.Join(directory, d.Related[j].Path)
			}
		}
		for j := range d.Fixes {
			f := &d.Fixes[j]
			for k := range f.Edits {
				e := &f.Edits[k]
				if !filepath.IsAbs(e.Path) {
					path := filepath.Join(options.WorkingDir, e.Path)
					if revision, ok := f.Revisions[e.Path]; ok {
						f.Revisions[path] = revision
						delete(f.Revisions, e.Path)
					}
					e.Path = path
				}
			}
		}
	}
	return langserver.Analysis{Diagnostics: result.Diagnostics, Sources: sources}, err
}

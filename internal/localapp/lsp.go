package localapp

import (
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/langserver"
	"actionlint.kjanat.dev/internal/workflow/editor"
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
	directory := options.WorkingDir
	if len(documents) == 0 {
		return langserver.Analysis{}, nil
	}
	opts := options
	sources := map[string]string{}
	var sourceMu sync.Mutex
	disk := fsread.New(ctx, options.ReadFile, nil)
	opts.ReadFile = func(path string) ([]byte, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if d, ok := documents[absolute]; ok {
			return []byte(d.Text), nil
		}
		content, err := disk.ReadFile(absolute)
		if err == nil {
			sourceMu.Lock()
			sources[absolute] = string(content)
			sourceMu.Unlock()
		}
		return content, err
	}
	result, err := application.Check(ctx, opts, application.Input{Kind: application.Files, Paths: slices.Sorted(maps.Keys(documents))})
	if result == nil {
		return langserver.Analysis{Sources: sources}, err
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

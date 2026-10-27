package localapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/langserver"
	sourceposition "actionlint.kjanat.dev/v2/internal/source/position"
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
	if ctx.Err() != nil {
		return langserver.Analysis{}, errors.Join(err, ctx.Err())
	}
	if result == nil {
		return langserver.Analysis{}, err
	}
	analysis, conversionErr := editorAnalysis(result, directory, documents)
	if ctx.Err() != nil {
		return langserver.Analysis{}, errors.Join(err, conversionErr, ctx.Err())
	}
	return analysis, errors.Join(err, conversionErr)
}

func editorAnalysis(result *application.Result, directory string, documents map[string]langserver.Document) (langserver.Analysis, error) {
	address := func(path string) string {
		if path == "" {
			return ""
		}
		if path != "<stdin>" && !filepath.IsAbs(path) {
			return filepath.Join(directory, path)
		}
		return path
	}
	sources := make(map[string]langserver.Source, len(result.CapturedSources))
	aliases := make(map[source.Revision][]string)
	missing := make(map[string]struct{})
	for _, input := range result.Inputs {
		path := address(input)
		if captured, consumed := result.CapturedSources[path]; consumed && (captured.Coordinates != source.YAML || !captured.CoordinatesAvailable) {
			missing[path] = struct{}{}
		}
	}
	for path, captured := range result.CapturedSources {
		if captured.Coordinates == source.YAML && captured.CoordinatesAvailable && captured.Revision.Valid() {
			sources[path] = langserver.Source{Text: string(captured.Content), Revision: captured.Revision, Coordinates: captured.Coordinates}
			aliases[captured.Revision] = append(aliases[captured.Revision], path)
		}
	}
	for revision := range aliases {
		slices.Sort(aliases[revision])
	}
	openAliases := make(map[string]struct{}, len(documents))
	for path := range documents {
		openAliases[address(path)] = struct{}{}
	}
	preferredAlias := func(revision source.Revision, preferred string) (string, bool) {
		candidates := aliases[revision]
		if len(candidates) == 0 {
			return "", false
		}
		if slices.Contains(candidates, preferred) {
			if _, open := openAliases[preferred]; open {
				return preferred, true
			}
		}
		for _, candidate := range candidates {
			if _, open := openAliases[candidate]; open {
				return candidate, true
			}
		}
		if slices.Contains(candidates, preferred) {
			return preferred, true
		}
		return candidates[0], true
	}
	diagnostics := make([]diagnostic.Diagnostic, 0, len(result.Diagnostics))
	for i, original := range result.Diagnostics {
		d := original
		originalPath := address(d.Path)
		if i >= len(result.Sources) || result.Sources[i].Unavailable {
			missing[originalPath] = struct{}{}
			continue
		}
		evidence := result.Sources[i]
		if evidence.RawScript && (!utf8.Valid(evidence.Content) || bytes.HasPrefix(evidence.Content, []byte("\ufeff")) || bytes.IndexAny(evidence.Content, "\r\u0085\u2028\u2029") >= 0) {
			missing[originalPath] = struct{}{}
			continue
		}
		var revision source.Revision
		if d.Reference != nil {
			revision, _ = source.NewRevision(d.Reference.Document, d.Reference.Revision)
		} else if captured, exists := result.CapturedSources[originalPath]; exists && bytes.Equal(evidence.Content, captured.Content) {
			revision = captured.Revision
		}
		path, admitted := preferredAlias(revision, originalPath)
		if !admitted || evidence.RawScript && !bytes.Equal(evidence.Content, result.CapturedSources[path].Content) {
			missing[originalPath] = struct{}{}
			continue
		}
		d.Path = path
		keptRelated := make([]diagnostic.Related, 0, len(d.Related))
		for _, related := range d.Related {
			originalAddress := address(related.Path)
			alias, admitted := preferredAlias(related.Revision, originalAddress)
			if !admitted || !related.Revision.Valid() || related.Coordinates != result.CapturedSources[alias].Coordinates {
				missing[originalAddress] = struct{}{}
				continue
			}
			if _, _, err := sourceposition.Range(result.CapturedSources[alias].Content, related.Coordinates, related.Start, related.End); err != nil {
				missing[originalAddress] = struct{}{}
				continue
			}
			related.Path = alias
			keptRelated = append(keptRelated, related)
		}
		d.Related = keptRelated
		d.Fixes = slices.Clone(d.Fixes)
		for j := range d.Fixes {
			f := &d.Fixes[j]
			f.Edits = slices.Clone(f.Edits)
			revisions := make(map[string]string, len(f.Revisions))
			consistent := true
			for editedPath, digest := range f.Revisions {
				normalized := address(editedPath)
				if normalized != "" && normalized == originalPath && digest == revision.Digest() {
					normalized = path
				}
				if previous, exists := revisions[normalized]; exists && previous != digest {
					consistent = false
				}
				revisions[normalized] = digest
			}
			f.Revisions = revisions
			if !consistent {
				f.Edits, f.Revisions = nil, nil
				missing[originalPath] = struct{}{}
				continue
			}
			for k := range f.Edits {
				e := &f.Edits[k]
				e.Path = address(e.Path)
				if e.Path != "" && e.Path == originalPath && revisions[path] == revision.Digest() {
					e.Path = path
				}
			}
		}
		diagnostics = append(diagnostics, d)
	}
	var err error
	if len(missing) != 0 {
		err = &editorEvidenceError{paths: slices.Sorted(maps.Keys(missing))}
	}
	return langserver.Analysis{Diagnostics: diagnostics, Sources: sources}, err
}

type editorEvidenceError struct{ paths []string }

func (e *editorEvidenceError) Error() string {
	return fmt.Sprintf("incomplete editor analysis: diagnostic sources unavailable for %d paths: %s", len(e.paths), strings.Join(e.paths, ", "))
}

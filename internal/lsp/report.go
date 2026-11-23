package lsp

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/languages/byteoffset"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
	"actionlint.kjanat.dev/v2/internal/results/suppression"
)

func FromReport(result *report.Analysis, directory string, documents map[string]Document) (Analysis, error) {
	address := func(path string) string {
		if path == "" {
			return ""
		}
		if path != "<stdin>" && !filepath.IsAbs(path) {
			return filepath.Join(directory, path)
		}
		return path
	}
	sources := make(map[string]Source, len(result.CapturedSources))
	aliases := make(map[sourcetext.Revision][]string)
	missing := make(map[string]struct{})
	for _, input := range result.Inputs {
		path := address(input)
		if captured, consumed := result.CapturedSources[path]; consumed && (captured.Coordinates != sourcetext.YAML || !captured.CoordinatesAvailable) {
			missing[path] = struct{}{}
		}
	}
	for path, captured := range result.CapturedSources {
		if captured.Coordinates == sourcetext.YAML && captured.CoordinatesAvailable && captured.Revision.Valid() {
			sources[path] = Source{Text: string(captured.Content), Revision: captured.Revision, Coordinates: captured.Coordinates}
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
	preferredAlias := func(revision sourcetext.Revision, preferred string) (string, bool) {
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
	allowed := make([]string, 0, len(result.Rules))
	for _, descriptor := range checking.ConfigurationRules().Rules() {
		allowed = append(allowed, descriptor.Name)
	}
	for _, descriptor := range result.Rules {
		if !slices.Contains(allowed, descriptor.Name) {
			allowed = append(allowed, descriptor.Name)
		}
	}
	diagnostics := make([]results.Diagnostic, 0, len(result.Diagnostics))
	for _, published := range result.Diagnostics {
		d, evidence := published.Diagnostic, published.Source
		originalPath := address(d.Path)
		if evidence.Unavailable.Present() {
			missing[originalPath] = struct{}{}
			continue
		}
		if evidence.RawScript && (!utf8.Valid(evidence.Content) || bytes.HasPrefix(evidence.Content, []byte("\ufeff")) || bytes.ContainsAny(evidence.Content, "\r\u0085\u2028\u2029")) {
			missing[originalPath] = struct{}{}
			continue
		}
		var revision sourcetext.Revision
		if d.Reference != nil {
			revision, _ = sourcetext.NewRevision(d.Reference.Document, d.Reference.Revision)
		} else if captured, exists := result.CapturedSources[originalPath]; exists && bytes.Equal(evidence.Content, captured.Content) {
			revision = captured.Revision
		}
		path, admitted := preferredAlias(revision, originalPath)
		if !admitted || evidence.RawScript && !bytes.Equal(evidence.Content, result.CapturedSources[path].Content) {
			missing[originalPath] = struct{}{}
			continue
		}
		d = d.Clone()
		d.Path = path
		keptRelated := make([]results.Related, 0, len(d.Related))
		for _, related := range d.Related {
			originalAddress := address(related.Path)
			alias, admitted := preferredAlias(related.Revision, originalAddress)
			if !admitted || !related.Revision.Valid() || related.Coordinates != result.CapturedSources[alias].Coordinates {
				missing[originalAddress] = struct{}{}
				continue
			}
			if _, _, err := byteoffset.Range(result.CapturedSources[alias].Content, related.Coordinates, related.Start, related.End); err != nil {
				missing[originalAddress] = struct{}{}
				continue
			}
			related.Path = alias
			keptRelated = append(keptRelated, related)
		}
		d.Related = keptRelated
		for j := range d.Fixes {
			f := &d.Fixes[j]
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
		if target := evidence.Suppression; target != nil {
			targetPath := address(target.Path)
			captured, exists := result.CapturedSources[targetPath]
			alias, admitted := preferredAlias(captured.Revision, targetPath)
			if exists && admitted && captured.Coordinates == sourcetext.YAML && captured.CoordinatesAvailable {
				if edit, ok := suppression.Edit(captured.Content, target.Lines.First, d.Rule, allowed, checking.RuleNames()); ok {
					edit.Path = alias
					d.Fixes = append(d.Fixes, results.Fix{
						Description: "Suppress " + d.Rule + " for this declaration", Applicability: applicability.Suggested,
						Revisions: map[string]string{alias: captured.Revision.Digest()}, Edits: []results.Edit{edit},
					})
				}
			}
		}
		diagnostics = append(diagnostics, d)
	}
	var err error
	if len(missing) != 0 {
		err = &UnavailableSourcesError{paths: slices.Sorted(maps.Keys(missing))}
	}
	return Analysis{Diagnostics: diagnostics, Sources: sources}, err
}

type UnavailableSourcesError struct{ paths []string }

func (e *UnavailableSourcesError) Error() string {
	return fmt.Sprintf("incomplete editor analysis: diagnostic sources unavailable for %d paths: %s", len(e.paths), strings.Join(e.paths, ", "))
}

func (e *UnavailableSourcesError) Paths() []string { return slices.Clone(e.paths) }

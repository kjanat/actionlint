package application

import (
	"errors"
	"fmt"
	"path/filepath"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

type admissionRejectedError struct {
	count int
}

func (e *admissionRejectedError) Error() string {
	return fmt.Sprintf("admission-rejected: withheld %d finding(s) without admitted reported sources", e.count)
}

func (*admissionRejectedError) Code() string { return "admission-rejected" }
func (e *admissionRejectedError) Count() int { return e.count }

func (s *analysisSession) admitSourceDocuments(composed composition.Result, references map[string]workflowfacts.SourceIndex) (map[string]bool, error) {
	rejected := map[string]bool{}
	var failure error
	for path, content := range composed.Sources {
		document, exists := composed.SourceDocuments[path]
		if !exists {
			failure = errors.Join(failure, fmt.Errorf("source %q has no admitted parse provenance", path))
			sourceRejected(rejected, path, capture.Snapshot{})
			address := path
			if path != "<stdin>" && !filepath.IsAbs(path) {
				address = filepath.Join(s.workingDir, path)
			}
			identity, err := s.captures.Canonical(s.ctx, address)
			if err != nil {
				failure = errors.Join(failure, fmt.Errorf("resolve rejected source %q: %w", path, err))
			} else {
				rejected[identity] = true
			}
			continue
		}
		if err := s.captures.Verify(document.Capture(), content); err != nil {
			failure = errors.Join(failure, err)
			sourceRejected(rejected, path, document.Capture())
			continue
		}
		nested, err := s.snapshots.Document(document.Capture(), document.Document())
		if err != nil {
			failure = errors.Join(failure, fmt.Errorf("admit source %q parse provenance: %w", path, err))
			sourceRejected(rejected, path, document.Capture())
			continue
		}
		references[path] = nested.CanonicalReferences()
	}
	return rejected, failure
}

func (s *analysisSession) admitReportedSources(all []*findingmodel.Finding, entryPath, sourcePath string, composed composition.Result, rejected map[string]bool) ([]*findingmodel.Finding, error) {
	var failure error
	count := 0
	metadataSources := make(map[source.Revision]bool, len(composed.SourceDocuments))
	for path, document := range composed.SourceDocuments {
		if _, consulted := composed.Sources[path]; consulted && s.captures.VerifyHandle(document.Capture()) == nil {
			metadataSources[document.Capture().Revision()] = true
		}
	}
	entryOwns := func(path string) bool {
		if path == "" || path == entryPath || path == sourcePath {
			return true
		}
		if composed.EntrySnapshot == nil {
			return false
		}
		entry := composed.EntrySnapshot.Reference()
		if path == entry.Document {
			return true
		}
		if relative, err := filepath.Rel(s.workingDir, entry.Document); err == nil && path == relative {
			return true
		}
		address := path
		if !filepath.IsAbs(address) {
			address = filepath.Join(s.workingDir, address)
		}
		captured, state, err := s.captures.Lookup(address)
		return err == nil && state == capture.Captured && captured.Revision().Document() == entry.Document && captured.Revision().Digest() == entry.Revision
	}
	accepted := all[:0]
	for _, finding := range all {
		bad := rejected[finding.Path]
		if finding.Reference != nil {
			bad = bad || rejected[finding.Reference.Document]
		}
		if target := finding.MetadataParseSource; target.Valid() {
			bad = bad || rejected[target.Document()] || !metadataSources[target]
		}
		if bad {
			count++
			continue
		}
		if finding.UnavailableSource.Present() {
			address := finding.UnavailableSource.Address()
			if address != "<stdin>" && !filepath.IsAbs(address) {
				address = filepath.Join(s.workingDir, address)
			}
			document, err := s.captures.Canonical(s.ctx, address)
			if err != nil {
				failure = errors.Join(failure, err)
				count++
				continue
			}
			marker, err := finding.UnavailableSource.BindDocument(document)
			if err != nil {
				failure = errors.Join(failure, err)
				count++
				continue
			}
			finding.UnavailableSource = marker
			finding.Reference, finding.Fixes = nil, nil
		} else if !entryOwns(finding.Path) {
			foreignEvidence := finding.Reference != nil
			if foreignEvidence && composed.EntrySnapshot != nil {
				foreignEvidence = finding.Reference.Document != composed.EntrySnapshot.Reference().Document
			}
			if !foreignEvidence {
				count++
				continue
			}
		}
		accepted = append(accepted, finding)
	}
	clear(all[len(accepted):])
	if count != 0 {
		failure = errors.Join(failure, &admissionRejectedError{count: count})
	}
	return accepted, failure
}

func sourceRejected(rejected map[string]bool, path string, captured capture.Snapshot) {
	rejected[path] = true
	if captured.Valid() {
		rejected[captured.Revision().Document()] = true
		for _, alias := range captured.Aliases() {
			rejected[alias] = true
		}
	}
}

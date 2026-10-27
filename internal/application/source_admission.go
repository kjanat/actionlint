package application

import (
	"errors"
	"fmt"
	"path/filepath"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
)

type admissionRejectedError struct {
	count int
}

func (e *admissionRejectedError) Error() string {
	return fmt.Sprintf("admission-rejected: withheld %d finding(s) without admitted reported sources", e.count)
}

func (*admissionRejectedError) Code() string { return "admission-rejected" }
func (e *admissionRejectedError) Count() int { return e.count }

func (s *analysisSession) admitReportedSources(all []*findingmodel.Finding, entryPath, sourcePath string, composed composition.Result, rejected map[string]bool) ([]*findingmodel.Finding, error) {
	var failure error
	count := 0
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
		if !finding.MetadataParseSource.Valid() && finding.MetadataParsePath != "" {
			// E11 removes this boundary bridge when every metadata producer carries
			// its captured parse source directly. No path lookup or IO is permitted.
			if document, exists := composed.SourceDocuments[finding.MetadataParsePath]; exists {
				finding.MetadataParseSource = document.Capture().Revision()
			}
		}
		bad := rejected[finding.Path]
		if finding.Reference != nil {
			bad = bad || rejected[finding.Reference.Document]
		}
		if finding.MetadataParseSource.Valid() {
			bad = bad || rejected[finding.MetadataParseSource.Document()]
		} else if finding.MetadataParsePath != "" {
			bad = true
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

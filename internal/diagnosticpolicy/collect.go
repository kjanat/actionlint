package diagnosticpolicy

import (
	"bytes"
	"reflect"
	"slices"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/source"
)

// File carries completed document findings and their diagnostic identity scope.
type File struct {
	Project, InputPath string
	Source             source.Revision
	Content            []byte
	Action             bool
	Findings           []*findingmodel.Finding
}

// Collect completes and deduplicates findings across documents in source order.
func Collect(files []File) (diagnostics []diagmodel.Diagnostic, sources []report.DiagnosticSource) {
	type location struct {
		project, path, rule string
		start               diagmodel.Position
	}
	seen := make(map[location][]diagmodel.Diagnostic)
	type metadataLocation struct {
		project, kind, severity string
		finding                 findingmodel.MetadataKey
	}
	seenMetadata := make(map[metadataLocation]bool)
	type declarationLocation struct {
		project string
		finding findingmodel.MetadataKey
	}
	declarations := make(map[declarationLocation]bool)
	parseDeclarations := make(map[source.Revision]bool)
	for _, file := range files {
		project := file.Project
		for _, finding := range file.Findings {
			if file.Action && file.Source.Valid() && finding.Rule == "syntax-check" {
				parseDeclarations[file.Source] = true
			}
			if identity := finding.MetadataIdentity; identity != nil && identity.Declaration && finding.Rule == "action" {
				declarations[declarationLocation{project, identity.Key}] = true
			}
		}
	}
	copies := sourceCopies{}
	for i := range files {
		file := &files[i]
		project := file.Project
		unique := file.Findings[:0]
		for _, finding := range file.Findings {
			if finding.Rule == "action" && finding.MetadataParseSource.Valid() && parseDeclarations[finding.MetadataParseSource] {
				continue
			}
			diagnostic := finding.Canonical(file.Content)
			identityPath := file.InputPath
			if finding.Source != nil {
				identityPath = diagnostic.Path
			}
			content := file.Content
			if finding.Foreign(file.Content) {
				content = copies.of(finding.Source)
			}
			CompleteDiagnostic(&diagnostic, content, identityPath, finding.UnavailableSource.Present())
			if identity := finding.MetadataIdentity; identity != nil && finding.Rule == "action" {
				key := metadataLocation{project, finding.Rule, finding.Severity, identity.Key}
				if !identity.Declaration && declarations[declarationLocation{project, identity.Key}] {
					continue
				}
				if seenMetadata[key] {
					continue
				}
				seenMetadata[key] = true
			}
			key := location{project, diagnostic.Path, diagnostic.Rule, diagnostic.Start}
			if slices.ContainsFunc(seen[key], func(previous diagmodel.Diagnostic) bool {
				return reflect.DeepEqual(previous, diagnostic)
			}) {
				continue
			}
			seen[key] = append(seen[key], diagnostic)
			diagnostics = append(diagnostics, diagnostic)
			sources = append(sources, report.DiagnosticSource{Content: content, RawScript: finding.RawScriptSource, Unavailable: finding.UnavailableSource.Present()})
			unique = append(unique, finding)
		}
		clear(file.Findings[len(unique):])
		file.Findings = unique
	}
	return diagnostics, sources
}

// sourceCopies copies each foreign source once per Collect call. Findings in
// the same action.yml or sourced script share one Source array, so their
// diagnostics share one copy instead of each holding the whole file.
type sourceCopies map[sourceKey][]byte

// sourceKey identifies a Source slice by its first byte and length.
type sourceKey struct {
	first *byte
	size  int
}

// of returns the copy of src, making it on first use. The copy shares no
// storage with any finding.
func (c sourceCopies) of(src []byte) []byte {
	if len(src) == 0 {
		return []byte{}
	}
	key := sourceKey{&src[0], len(src)}
	if copied, ok := c[key]; ok {
		return copied
	}
	copied := bytes.Clone(src)
	c[key] = copied
	return copied
}

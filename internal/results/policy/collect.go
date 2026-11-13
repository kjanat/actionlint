package policy

import (
	"bytes"
	"reflect"
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// File carries completed document findings and their diagnostic identity scope.
type File struct {
	Project, InputPath string
	Source             sourcepos.Revision
	Content            []byte
	Action             bool
	Findings           []*findingmodel.Finding
}

// Collect completes and deduplicates findings across documents in source order.
func (processor Processor) Collect(files []File) (diagnostics []diagmodel.Diagnostic, sources []report.DiagnosticSource) {
	type location struct {
		project, path, rule string
		start               sourcepos.Position
	}
	seen := make(map[location][]int)
	var retained []*findingmodel.Finding
	mergeBlocking := func(index int, diagnostic diagmodel.Diagnostic) {
		diagnostics[index].NonBlocking = diagnostics[index].NonBlocking && diagnostic.NonBlocking
		retained[index].NonBlocking = diagnostics[index].NonBlocking
	}
	type metadataLocation struct {
		project, kind, severity string
		finding                 findingmodel.MetadataKey
	}
	seenMetadata := make(map[metadataLocation]int)
	type declarationLocation struct {
		project string
		finding findingmodel.MetadataKey
	}
	declarations := make(map[declarationLocation]bool)
	parseDeclarations := make(map[sourcepos.Revision]bool)
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
			processor.CompleteDiagnostic(&diagnostic, content, identityPath, finding.UnavailableSource.Present())
			var metadataKey metadataLocation
			isMetadata := finding.MetadataIdentity != nil && finding.Rule == "action"
			if identity := finding.MetadataIdentity; isMetadata {
				metadataKey = metadataLocation{project, finding.Rule, finding.Severity, identity.Key}
				if !identity.Declaration && declarations[declarationLocation{project, identity.Key}] {
					continue
				}
				if index, exists := seenMetadata[metadataKey]; exists {
					mergeBlocking(index, diagnostic)
					continue
				}
			}
			key := location{project, diagnostic.Path, diagnostic.Rule, diagnostic.Start}
			duplicate := slices.IndexFunc(seen[key], func(index int) bool {
				previous, current := diagnostics[index], diagnostic
				previous.NonBlocking, current.NonBlocking = false, false
				return reflect.DeepEqual(previous, current)
			})
			if duplicate >= 0 {
				index := seen[key][duplicate]
				mergeBlocking(index, diagnostic)
				if isMetadata {
					seenMetadata[metadataKey] = index
				}
				continue
			}
			if isMetadata {
				seenMetadata[metadataKey] = len(diagnostics)
			}
			seen[key] = append(seen[key], len(diagnostics))
			retained = append(retained, finding)
			diagnostics = append(diagnostics, diagnostic)
			var target *report.SuppressionTarget
			if suppression := finding.SuppressionTarget; suppression != nil {
				path := suppression.Path
				if path == "" {
					path = file.InputPath
				}
				target = &report.SuppressionTarget{Path: path, First: suppression.First, Last: suppression.Last}
			}
			sources = append(sources, report.DiagnosticSource{Content: content, RawScript: finding.RawScriptSource, Unavailable: finding.UnavailableSource.Present(), Suppression: target})
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

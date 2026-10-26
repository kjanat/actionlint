package diagnosticpolicy

import (
	"reflect"
	"slices"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/report"
)

// File carries completed document findings and their diagnostic identity scope.
type File struct {
	Project, InputPath string
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
	parseDeclarations := make(map[string]bool)
	for _, file := range files {
		project := file.Project
		for _, finding := range file.Findings {
			if file.Action && finding.Rule == "syntax-check" {
				parseDeclarations[fsread.PhysicalPath(file.InputPath)] = true
			}
			if identity := finding.MetadataIdentity; identity != nil && identity.Declaration && finding.Rule == "action" {
				declarations[declarationLocation{project, identity.Key}] = true
			}
		}
	}
	for i := range files {
		file := &files[i]
		project := file.Project
		unique := file.Findings[:0]
		for _, finding := range file.Findings {
			if finding.Rule == "action" && finding.MetadataParsePath != "" && parseDeclarations[fsread.PhysicalPath(finding.MetadataParsePath)] {
				continue
			}
			diagnostic := finding.Canonical(file.Content)
			identityPath := file.InputPath
			if finding.Source != nil {
				identityPath = diagnostic.Path
			}
			CompleteDiagnostic(&diagnostic, finding.SourceFor(file.Content), identityPath)
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
			sources = append(sources, report.DiagnosticSource{Content: finding.SourceFor(file.Content), RawScript: finding.RawScriptSource})
			unique = append(unique, finding)
		}
		clear(file.Findings[len(unique):])
		file.Findings = unique
	}
	return diagnostics, sources
}

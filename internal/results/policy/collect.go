package policy

import (
	"bytes"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// retain makes the only visibility and duplicate decision across evaluated batches.
func retain(files []evaluatedBatch, authority map[*finding.Finding]obligation) (diagnostics []results.PublishedDiagnostic) {
	type location struct {
		project, path, rule string
		start               sourcetext.Position
	}
	seen := make(map[location][]int)
	dependencySources := make(map[location][]int)
	var retained []*finding.Finding
	var retainedBatch []int
	mergeFinding := func(index int, diagnostic results.Diagnostic) {
		if diagnostic.Origin == results.ProjectOrigin {
			diagnostics[index].Origin = results.ProjectOrigin
			retained[index].Origin = results.ProjectOrigin
		}
		diagnostics[index].NonBlocking = diagnostics[index].NonBlocking && diagnostic.NonBlocking
		retained[index].NonBlocking = diagnostics[index].NonBlocking
		if _, required := authority[retained[index]]; required {
			retained[index].Source.Suppression = nil
			diagnostics[index].Source.Suppression = nil
		}
	}
	type metadataLocation struct {
		project, kind string
		severity      severity.Severity
		finding       finding.MetadataKey
	}
	seenMetadata := make(map[metadataLocation][]int)
	type declarationLocation struct {
		project, rule string
		finding       finding.MetadataKey
	}
	declarations := make(map[declarationLocation]bool)
	parseDeclarations := make(map[sourcetext.Revision]bool)
	for _, file := range files {
		project := file.Project
		for _, finding := range file.Findings {
			if file.Action && file.Source.Valid() && finding.Rule == "syntax-check" {
				parseDeclarations[file.Source] = true
			}
			if identity := finding.MetadataIdentity; identity != nil && identity.Declaration {
				declarations[declarationLocation{project, finding.Rule, identity.Key}] = true
			}
		}
	}
	copies := sourceCopies{}
	for i := range files {
		file := &files[i]
		project := file.Project
		type invocationIdentity struct {
			metadata  metadataLocation
			authority obligation
		}
		preferred := make(map[invocationIdentity]*finding.Finding)
		for _, candidate := range file.Findings {
			if identity := candidate.MetadataIdentity; identity != nil {
				key := invocationIdentity{metadataLocation{project, candidate.Rule, candidate.Severity, identity.Key}, authority[candidate]}
				prior := preferred[key]
				if prior == nil {
					preferred[key] = candidate
					continue
				}
				blocking := prior.NonBlocking && candidate.NonBlocking
				if identity.Visit < prior.MetadataIdentity.Visit {
					preferred[key] = candidate
				}
				preferred[key].NonBlocking = blocking
			}
		}
		unique := file.Findings[:0]
		for _, finding := range file.Findings {
			if identity := finding.MetadataIdentity; identity != nil {
				key := invocationIdentity{metadataLocation{project, finding.Rule, finding.Severity, identity.Key}, authority[finding]}
				if preferred[key] != finding {
					continue
				}
			}
			_, required := authority[finding]
			if !required && finding.MetadataParseSource.Valid() && parseDeclarations[finding.MetadataParseSource] {
				continue
			}
			published := finding.Publish(file.Policy.Content)
			identityPath := file.InputPath
			if finding.Source.Content != nil {
				identityPath = published.Path
			}
			if finding.Foreign(file.Policy.Content) {
				published.Source.Content = copies.of(finding.Source.Content)
			}
			file.Processor.completeDiagnostic(&published.Diagnostic, published.Source.Content, identityPath, published.Source.Unavailable.Present())
			diagnostic := published.Diagnostic
			if source, _, ok := results.DependencyAddress(diagnostic.Path); ok && diagnostic.Origin == results.DependencyOrigin {
				key := location{project, source, diagnostic.Rule, diagnostic.Start}
				duplicate := slices.IndexFunc(dependencySources[key], func(index int) bool {
					return authority[retained[index]] == authority[finding] && identicalDependencyFinding(diagnostics[index], published)
				})
				if duplicate >= 0 {
					index := dependencySources[key][duplicate]
					mergeFinding(index, diagnostic)
					related := results.Related{Message: results.IdenticalDependencyLocation, Path: diagnostic.Path, Start: diagnostic.Start, End: diagnostic.End}
					if !slices.Contains(diagnostics[index].Related, related) {
						diagnostics[index].Related = append(diagnostics[index].Related, related)
						retained[index].Related = append(retained[index].Related, related)
					}
					continue
				}
			}
			var metadataKey metadataLocation
			isMetadata := finding.MetadataIdentity != nil
			if identity := finding.MetadataIdentity; isMetadata {
				metadataKey = metadataLocation{project, finding.Rule, finding.Severity, identity.Key}
				if _, required := authority[finding]; !required && !identity.Declaration && declarations[declarationLocation{project, finding.Rule, identity.Key}] {
					continue
				}
				duplicate := slices.IndexFunc(seenMetadata[metadataKey], func(index int) bool {
					previous := retained[index]
					return authority[previous] == authority[finding]
				})
				if duplicate >= 0 {
					mergeFinding(seenMetadata[metadataKey][duplicate], diagnostic)
					continue
				}
			}
			key := location{project, diagnostic.Path, diagnostic.Rule, diagnostic.Start}
			duplicate := slices.IndexFunc(seen[key], func(index int) bool {
				previous, current := diagnostics[index].Diagnostic, diagnostic
				previous.Related, current.Related = withoutDependencyLocations(previous.Related), withoutDependencyLocations(current.Related)
				previous.NonBlocking, current.NonBlocking = false, false
				previous.Origin, current.Origin = "", ""
				if retainedBatch[index] == i && retained[index].InferredReference && finding.InferredReference {
					previous.Reference, current.Reference = nil, nil
				}
				return authority[retained[index]] == authority[finding] && bytes.Equal(diagnostics[index].Source.Content, published.Source.Content) && reflect.DeepEqual(previous, current)
			})
			if duplicate >= 0 {
				index := seen[key][duplicate]
				mergeFinding(index, diagnostic)
				if isMetadata {
					seenMetadata[metadataKey] = append(seenMetadata[metadataKey], index)
				}
				continue
			}
			if isMetadata {
				seenMetadata[metadataKey] = append(seenMetadata[metadataKey], len(diagnostics))
			}
			seen[key] = append(seen[key], len(diagnostics))
			retained = append(retained, finding)
			retainedBatch = append(retainedBatch, i)
			if suppression := finding.Source.Suppression; suppression != nil && !required {
				target := *suppression
				if target.Path == "" {
					target.Path = file.InputPath
				}
				published.Source.Suppression = &target
			}
			if source, _, ok := results.DependencyAddress(diagnostic.Path); ok && diagnostic.Origin == results.DependencyOrigin {
				key := location{project, source, diagnostic.Rule, diagnostic.Start}
				dependencySources[key] = append(dependencySources[key], len(diagnostics))
			}
			diagnostics = append(diagnostics, published)
			unique = append(unique, finding)
		}
		clear(file.Findings[len(unique):])
		file.Findings = unique
	}
	return diagnostics
}

// sourceCopies copies each foreign source once per publication. Findings in
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

package policy

import (
	"bytes"
	"reflect"
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// retain makes the only visibility and duplicate decision across evaluated batches.
func retain(files []evaluatedBatch, authority map[*findingmodel.Finding]obligation) (diagnostics []diagmodel.PublishedDiagnostic) {
	type location struct {
		project, path, rule string
		start               sourcepos.Position
	}
	seen := make(map[location][]int)
	var retained []*findingmodel.Finding
	var retainedBatch []int
	mergeFinding := func(index int, diagnostic diagmodel.Diagnostic) {
		diagnostics[index].NonBlocking = diagnostics[index].NonBlocking && diagnostic.NonBlocking
		retained[index].NonBlocking = diagnostics[index].NonBlocking
		if _, required := authority[retained[index]]; required {
			retained[index].Source.Suppression = nil
			diagnostics[index].Source.Suppression = nil
		}
	}
	type metadataLocation struct {
		project, kind, severity string
		finding                 findingmodel.MetadataKey
	}
	seenMetadata := make(map[metadataLocation][]int)
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
		type invocationIdentity struct {
			metadata  metadataLocation
			authority obligation
		}
		preferred := make(map[invocationIdentity]*findingmodel.Finding)
		for _, candidate := range file.Findings {
			if identity := candidate.MetadataIdentity; identity != nil && candidate.Rule == "action" {
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
			if identity := finding.MetadataIdentity; identity != nil && finding.Rule == "action" {
				key := invocationIdentity{metadataLocation{project, finding.Rule, finding.Severity, identity.Key}, authority[finding]}
				if preferred[key] != finding {
					continue
				}
			}
			_, required := authority[finding]
			if !required && finding.Rule == "action" && finding.MetadataParseSource.Valid() && parseDeclarations[finding.MetadataParseSource] {
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
			var metadataKey metadataLocation
			isMetadata := finding.MetadataIdentity != nil && finding.Rule == "action"
			if identity := finding.MetadataIdentity; isMetadata {
				metadataKey = metadataLocation{project, finding.Rule, finding.Severity, identity.Key}
				if _, required := authority[finding]; !required && !identity.Declaration && declarations[declarationLocation{project, identity.Key}] {
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
				previous.NonBlocking, current.NonBlocking = false, false
				if retainedBatch[index] == i && retained[index].InferredReference && finding.InferredReference {
					previous.Reference, current.Reference = nil, nil
				}
				return authority[retained[index]] == authority[finding] && reflect.DeepEqual(previous, current)
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

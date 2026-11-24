package policy

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func InferReferences(all []*finding.Finding, path string, references map[string]facts.SourceIndex, entryAliases ...string) {
	for _, finding := range all {
		if finding.Source.Unavailable.Present() {
			finding.Reference, finding.Fixes = nil, nil
			continue
		}
		if finding.Reference != nil {
			continue
		}
		filename := path
		if finding.Path != "" && finding.Path != path && !slices.Contains(entryAliases, finding.Path) {
			filename = finding.Path
		}
		refs, ok := references[filename]
		if !ok {
			continue
		}
		ref := refs.Document
		if at, exists := refs.Positions[[2]int{finding.Start.Line, finding.Start.Column}]; exists {
			ref = at
		}
		site := finding.Site
		if site != nil && !finding.Source.RawScript {
			if job, exists := refs.Jobs[strings.ToLower(site.Job)]; exists {
				ref = job
			}
			if site.Step != nil {
				if step, exists := refs.Positions[[2]int{site.Step.Line, site.Step.Column}]; exists {
					ref = step
				}
			}
		}

		finding.Reference = &ref
		finding.InferredReference = true
	}
}

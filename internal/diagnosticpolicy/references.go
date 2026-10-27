package diagnosticpolicy

import (
	"slices"
	"strings"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
)

// InferReferences enriches completed findings from source indexes supplied by the caller.
func InferReferences(all []*findingmodel.Finding, path string, references map[string]workflowfacts.References, entryAliases ...string) {
	for _, finding := range all {
		if finding.UnavailableSource.Present() {
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
		owner := finding.Owner
		if owner != nil && !finding.RawScriptSource {
			if job, exists := refs.Jobs[strings.ToLower(owner.Job)]; exists {
				ref = job
			}
			if owner.Step != nil {
				if step, exists := refs.Positions[[2]int{owner.Step.Line, owner.Step.Col}]; exists {
					ref = step
				}
			}
		}

		finding.Reference = &ref
		finding.InferredReference = true
	}
}

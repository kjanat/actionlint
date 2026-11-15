package policy

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// Batch is one admitted analysis and the settings prepared before its rules ran.
// Sources and References must contain only sources accepted by final capture validation.
type Batch struct {
	Processor          Processor
	Policy             Policy
	Project, InputPath string
	Source             sourcepos.Revision
	Action             bool
	Findings           []*findingmodel.Finding
	Sources            map[string][]byte
	References         map[string]workflowfacts.SourceIndex
	EntryAliases       []string
}

// Publication contains the retained findings in batch order and their public projections.
type Publication struct {
	Diagnostics      []results.Diagnostic
	Sources          []report.DiagnosticSource
	Findings         [][]*findingmodel.Finding
	IncompleteChecks []report.IncompleteCheck
}

// obligation is issued by a policy evaluator, never by a diagnostic producer.
// Its identity preserves distinct requirements during domain-independent retention.
type obligation struct{ domain, identity string }

type evaluatedBatch struct{ Batch }

// Publish evaluates all admitted batches together, reconciles suppression use,
// and makes the final retention decision. It does not change the input findings.
func Publish(batches []Batch, incomplete []report.IncompleteCheck) (Publication, error) {
	incomplete = slices.Clone(incomplete)
	files := make([]evaluatedBatch, len(batches))
	authority := make(map[*findingmodel.Finding]obligation)
	usage := suppressionUses{}
	sourceCopies := sourceCopies{}
	records := findingmodel.NewCopier()
	failedSources := make(map[checkRevision]bool)
	for _, check := range incomplete {
		if check.Rule == "" {
			failedSources[checkRevision{check.Source.Document, check.Source.Revision}] = true
		}
	}
	markIncomplete := func(batch Batch) {
		observe := func(ref sourcepos.Reference) {
			key := checkRevision{ref.Document, ref.Revision}
			if ref.Document != "" && ref.Revision != "" && !failedSources[key] {
				failedSources[key] = true
				incomplete = append(incomplete, report.IncompleteCheck{Source: ref, Reason: "policy evaluation failed"})
			}
		}
		if batch.Source.Valid() {
			observe(sourcepos.Reference{Document: batch.Source.Document(), Revision: batch.Source.Digest()})
		}
		for _, path := range slices.Sorted(maps.Keys(batch.References)) {
			observe(batch.References[path].Document)
		}
	}
	var resultErr error
	for i, batch := range batches {
		files[i].Batch = batch
		files[i].Findings = nil
		if batch.Policy.Scope == nil || batch.Policy.Bindings == nil {
			if len(batch.Findings) != 0 {
				resultErr = errors.Join(resultErr, fmt.Errorf("analysis %q has no prepared publication policy", batch.InputPath))
				markIncomplete(batch)
			}
			continue
		}
		input := make([]*findingmodel.Finding, len(batch.Findings))
		for j, finding := range batch.Findings {
			input[j] = records.Clone(finding)
		}
		p := batch.Policy
		if p.Content != nil {
			p.Content = sourceCopies.of(p.Content)
		}
		p.usedSuppression = func(path string, position sourcepos.Position, rule string) {
			if index, ok := batch.References[path]; ok {
				usage.observe(index.Document, position, rule)
			}
		}
		sources := maps.Clone(batch.Sources)
		if sources == nil {
			sources = make(map[string][]byte)
		}
		for path, content := range sources {
			if content != nil {
				sources[path] = sourceCopies.of(content)
			}
		}
		findings, err := batch.Processor.evaluate(p, input, sources, authority)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			markIncomplete(batch)
			continue
		}
		InferReferences(findings, p.ConfigPath, batch.References, batch.EntryAliases...)
		files[i].Policy = p
		files[i].Findings = findings
	}
	usage.discardUnproven(files, incomplete)
	addUnusedSuppressionFixes(files)
	diagnostics, sources := retain(files, authority)
	result := Publication{Diagnostics: diagnostics, Sources: sources, Findings: make([][]*findingmodel.Finding, len(files)), IncompleteChecks: incomplete}
	for i, file := range files {
		result.Findings[i] = file.Findings
	}
	return result, resultErr
}

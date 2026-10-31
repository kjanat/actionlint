// Package documentparse decodes workflow and action sources into analysis inputs.
// It has no configuration, runtime, registration or policy dependencies.
package documentparse

import (
	"bytes"
	"errors"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	documentmodel "actionlint.kjanat.dev/v2/document"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

// Document is the plain result of decoding one workflow or action source.
// Its fields are caller-owned decode outputs with no invariants between them;
// the only sealed part is Provenance, which SourceSnapshots trusts on admission.
type Document struct {
	// Workflow is the decoded workflow; nil for action documents.
	Workflow *workflowlang.Workflow
	// Metadata is the decoded action manifest; nil for workflow documents.
	Metadata *actionmeta.Metadata
	Outline  documentmodel.Outline
	Findings []*findingmodel.Finding
	// Facts supplies declaration projections: the workflow itself, or the
	// projection of an action's steps. Action projections are never workflow
	// traversal inputs. It is nil when decoding recovered no semantic input.
	Facts *workflowlang.Workflow
	// ParseError holds action decode and admission failures, preserving their causes.
	ParseError error
	// Provenance is the parse record SourceSnapshots binds on admission.
	Provenance Provenance
}

// Provenance seals the source text and declarations published by one decode.
// Only this package creates it; its zero value has no parse provenance.
type Provenance struct{ record *declaration.Record }

func Workflow(path string, content []byte) Document {
	owned := bytes.Clone(content)
	w, findings := Parse(owned)
	return Document{
		Workflow: w, Facts: w, Outline: WorkflowOutline(path, w, len(findings) != 0), Findings: findings,
		Provenance: Provenance{declaration.Publish(string(owned), w, false, "")},
	}
}

func Action(path, sourcePath string, content []byte) Document {
	owned := bytes.Clone(content)
	metadata, err := actionmeta.Parse(sourcePath, owned)
	document := Document{Metadata: metadata, Outline: ActionOutline(path, metadata, err), ParseError: err}
	var provenance *workflowlang.Workflow
	_, rejected := errors.AsType[*codec.AdmissionError](err)
	if metadata != nil && !rejected {
		provenance = actionFacts(metadata)
	}
	if err != nil {
		document.Findings = adaptSyntaxFindings(workflowparser.HandleYAMLUnmarshalError(err))
		for _, finding := range document.Findings {
			finding.Start.Line, finding.Start.Column = max(1, finding.Start.Line), max(1, finding.Start.Column)
		}
	} else {
		document.Facts = provenance
	}
	if metadata != nil {
		document.Provenance = Provenance{declaration.Publish(string(owned), provenance, true, metadata.Runs.Using)}
	}
	return document
}

// CanAnalyze reports whether decoding recovered a semantic input for either language.
func (d Document) CanAnalyze() bool { return d.Facts != nil }

func actionFacts(metadata *actionmeta.Metadata) *workflowlang.Workflow {
	parser := workflowparser.NewSourceParser(metadata.Source)
	parser.IndexScalarSources(metadata.Root)
	job := &workflowlang.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, actionStep(step, parser))
	}
	workflow := &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": job}}
	parser.IndexDependencySpans(workflow)
	return workflow
}

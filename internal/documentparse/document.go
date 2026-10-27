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

type Document struct {
	Workflow *workflowlang.Workflow
	Metadata *actionmeta.Metadata
	Outline  documentmodel.Outline
	Findings []*findingmodel.Finding
	facts    *workflowlang.Workflow
	record   *declaration.Record
	parseErr error
}

func Workflow(path string, content []byte) Document {
	owned := bytes.Clone(content)
	w, findings := Parse(owned)
	return Document{Workflow: w, Outline: WorkflowOutline(path, w, len(findings) != 0), Findings: findings, record: declaration.Publish(string(owned), w, false, "")}
}

func Action(path, sourcePath string, content []byte) Document {
	owned := bytes.Clone(content)
	metadata, err := actionmeta.Parse(sourcePath, owned)
	document := Document{Metadata: metadata, Outline: ActionOutline(path, metadata, err), parseErr: err}
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
		document.facts = provenance
	}
	if metadata != nil {
		document.record = declaration.Publish(string(owned), provenance, true, metadata.Runs.Using)
	}
	return document
}

// ActionParseError returns decode and admission failures, preserving their causes.
func (d Document) ActionParseError() error { return d.parseErr }

// CanAnalyze reports whether decoding recovered a semantic input for either language.
func (d Document) CanAnalyze() bool { return d.Workflow != nil || d.facts != nil }

// Facts supplies declaration projections; action projections are never workflow traversal inputs.
func (d Document) Facts() *workflowlang.Workflow {
	if d.Metadata != nil {
		return d.facts
	}
	return d.Workflow
}

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

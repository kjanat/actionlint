// Package documentparse decodes workflow and action sources into analysis inputs.
// It has no configuration, runtime, registration or policy dependencies.
package documentparse

import (
	"bytes"

	documentmodel "actionlint.kjanat.dev/v2/document"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse/internal/frozen"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type Document struct {
	Workflow  *workflowlang.Workflow
	Metadata  *actionmeta.Metadata
	Outline   documentmodel.Outline
	Findings  []*findingmodel.Finding
	facts     *workflowlang.Workflow
	record    *frozen.Record
	traversal *workflowlang.Workflow
	parseErr  error
}

func Workflow(path string, content []byte) Document {
	owned := bytes.Clone(content)
	w, findings := Parse(owned)
	return Document{Workflow: w, Outline: WorkflowOutline(path, w, len(findings) != 0), Findings: findings, record: frozen.Publish(string(owned), w, false, ""), traversal: w}
}

func Action(path, sourcePath string, content []byte) Document {
	owned := bytes.Clone(content)
	metadata, err := actionmeta.Parse(sourcePath, owned)
	document := Document{Metadata: metadata, Outline: ActionOutline(path, metadata, err), parseErr: err}
	var provenance *workflowlang.Workflow
	if metadata != nil {
		provenance = actionFacts(metadata)
	}
	if err != nil {
		document.Findings = adaptSyntaxFindings(workflowlang.HandleYAMLUnmarshalError(err))
		for _, finding := range document.Findings {
			finding.Start.Line, finding.Start.Column = max(1, finding.Start.Line), max(1, finding.Start.Column)
		}
	} else {
		document.facts = provenance
	}
	if metadata != nil {
		document.record = frozen.Publish(string(owned), provenance, true, metadata.Runs.Using)
	}
	document.traversal = document.facts
	return document
}

// ActionParseError returns the original metadata decode error, including when
// the recovered syntax supplies source provenance only.
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
	parser := workflowlang.NewSourceParser(metadata.Source)
	parser.IndexScalarSources(metadata.Root)
	job := &workflowlang.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, ActionStep(step, parser))
	}
	return &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": job}}
}

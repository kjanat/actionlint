// Package parsing decodes workflow and action sources into analysis inputs.
// It has no configuration, runtime, registration or policy dependencies.
package parsing

import (
	"bytes"
	"errors"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Document is one decode of a source's bytes. Over the captured bytes it adds
// the decoded workflow or action metadata, syntax and admission failures, and
// the sealed Provenance that SourceSnapshots binds on admission. It names no
// document: identity comes from the capture at admission. Declaration fields
// belong to the caller.
type Document struct {
	kind DocumentKind
	// Workflow is the decoded workflow; nil for action documents.
	Workflow *definition.Workflow
	// Metadata is the decoded action manifest; nil for workflow documents.
	Metadata     *actionmeta.Metadata
	SyntaxErrors []*workflowparser.SyntaxError
	// Facts supplies declaration projections: the workflow itself, or the
	// projection of an action's steps. Action projections are never workflow
	// traversal inputs. It is nil when decoding recovered no semantic input.
	Facts *definition.Workflow
	// ParseError holds action decode and admission failures, preserving their causes.
	ParseError error
	// Provenance is the parse record SourceSnapshots binds on admission.
	Provenance Provenance
}

// Provenance seals the source text and declarations published by one decode,
// before a registry assigns them a document identity. Only this package
// creates it, so admission cannot bind a hand-built tree to captured bytes;
// its zero value has no parse provenance. SourceSnapshots binds it to a
// declaration.Bound once the capture names the document.
type Provenance struct {
	parsed   bool
	text     string
	workflow *definition.Workflow
	action   bool
	using    string
}

func (p Provenance) bind(identity string) *workflows.Bound {
	return workflows.Bind(identity, p.text, p.workflow, p.action, p.using)
}

func Workflow(content []byte) Document {
	owned := bytes.Clone(content)
	w, issues := workflowparser.ParseWorkflow(owned)
	return Document{
		kind: KindWorkflow, Workflow: w, Facts: w, SyntaxErrors: issues,
		Provenance: Provenance{parsed: true, text: string(owned), workflow: w},
	}
}

func Action(sourcePath string, content []byte) Document {
	owned := bytes.Clone(content)
	metadata, err := actionmeta.ParseActionMetadata(sourcePath, owned)
	document := Document{kind: KindAction, Metadata: metadata, ParseError: err}
	var provenance *definition.Workflow
	_, rejected := errors.AsType[*codec.AdmissionError](err)
	if metadata != nil && !rejected {
		provenance = actionFacts(metadata)
	}
	if err != nil {
		document.SyntaxErrors = workflowparser.HandleYAMLUnmarshalError(err)
		for _, issue := range document.SyntaxErrors {
			issue.Line, issue.Column = max(1, issue.Line), max(1, issue.Column)
		}
	} else {
		document.Facts = provenance
	}
	if metadata != nil {
		document.Provenance = Provenance{parsed: true, text: string(owned), workflow: provenance, action: true, using: metadata.Runs.Using}
	}
	return document
}

// Kind returns the language selected when parsing started, even when parsing
// produced no declarations.
func (d Document) Kind() DocumentKind { return d.kind }

// ParseStatus reports whether parsing produced complete, partial or no declarations.
// It does not establish whether the declared workflow or action can execute.
func (d Document) ParseStatus() string {
	switch d.kind {
	case KindWorkflow:
		if d.Workflow == nil {
			return "failed"
		}
		if len(d.SyntaxErrors) != 0 {
			return "partial"
		}
	case KindAction:
		if d.Metadata == nil || d.Metadata.Root == nil {
			return "failed"
		}
		if d.ParseError != nil {
			return "partial"
		}
	default:
		return "failed"
	}
	return "complete"
}

// CanAnalyze reports whether decoding recovered a semantic input for either language.
func (d Document) CanAnalyze() bool { return d.Facts != nil }

func actionFacts(metadata *actionmeta.Metadata) *definition.Workflow {
	parser := workflowparser.NewSourceParser(metadata.Source)
	parser.IndexScalarSources(metadata.Root)
	job := &definition.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, actionStep(step, parser))
	}
	// Composite output values are evaluated after its steps, like job outputs.
	// Reuse the validated fields so schema recovery and source positions agree.
	for _, field := range actionmeta.Validate(metadata).ExpressionFields {
		if field.SchemaPath != "outputs.*.value" || field.IsKey {
			continue
		}
		if job.Outputs == nil {
			job.Outputs = make(map[string]*definition.Output)
		}
		job.Outputs[field.FieldPath] = &definition.Output{Value: &definition.String{Value: field.Value.Value, Pos: &source.Position{Line: field.Value.Line, Column: field.Value.Column}}}
	}
	workflow := &definition.Workflow{Jobs: map[string]*definition.Job{"": job}, ExpressionSources: parser.ExpressionSources(metadata.Root)}
	parser.IndexDependencySpans(workflow)
	return workflow
}

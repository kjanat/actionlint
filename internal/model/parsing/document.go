// Package parsing decodes workflow and action sources into analysis inputs.
// It has no configuration, runtime, registration or policy dependencies.
package parsing

import (
	"bytes"
	"errors"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// Document contains decoded declarations, syntax failures and parse provenance.
// Declaration fields belong to the caller. SourceSnapshots verifies Provenance
// before publishing declaration views.
type Document struct {
	kind Kind
	// Workflow is the decoded workflow; nil for action documents.
	Workflow *workflowlang.Workflow
	// Metadata is the decoded action manifest; nil for workflow documents.
	Metadata     *actionmeta.Metadata
	SyntaxErrors []*workflowparser.SyntaxError
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

func Workflow(content []byte) Document {
	owned := bytes.Clone(content)
	w, issues := workflowparser.Parse(owned)
	return Document{
		kind: WorkflowKind, Workflow: w, Facts: w, SyntaxErrors: issues,
		Provenance: Provenance{declaration.Publish(string(owned), w, false, "")},
	}
}

func Action(sourcePath string, content []byte) Document {
	owned := bytes.Clone(content)
	metadata, err := actionmeta.Parse(sourcePath, owned)
	document := Document{kind: ActionKind, Metadata: metadata, ParseError: err}
	var provenance *workflowlang.Workflow
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
		document.Provenance = Provenance{declaration.Publish(string(owned), provenance, true, metadata.Runs.Using)}
	}
	return document
}

// Kind identifies the document language even when parsing produced no declarations.
type Kind string

const (
	WorkflowKind Kind = "workflow"
	ActionKind   Kind = "action"
)

// Kind returns the language selected when parsing started.
func (d Document) Kind() Kind { return d.kind }

// ParseStatus reports whether parsing produced complete, partial or no declarations.
// It does not establish whether the declared workflow or action can execute.
func (d Document) ParseStatus() string {
	switch d.kind {
	case WorkflowKind:
		if d.Workflow == nil {
			return "failed"
		}
		if len(d.SyntaxErrors) != 0 {
			return "partial"
		}
	case ActionKind:
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

func actionFacts(metadata *actionmeta.Metadata) *workflowlang.Workflow {
	parser := workflowparser.NewSourceParser(metadata.Source)
	parser.IndexScalarSources(metadata.Root)
	job := &workflowlang.Job{}
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
			job.Outputs = make(map[string]*workflowlang.Output)
		}
		job.Outputs[field.FieldPath] = &workflowlang.Output{Value: &workflowlang.String{Value: field.Value.Value, Pos: &workflowlang.Pos{Line: field.Value.Line, Col: field.Value.Column}}}
	}
	workflow := &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": job}, ExpressionSources: parser.ExpressionSources(metadata.Root)}
	parser.IndexDependencySpans(workflow)
	return workflow
}

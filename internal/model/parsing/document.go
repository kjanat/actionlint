// Package parsing decodes workflow and action sources and depends on no configuration, runtime, registration or policy package.
package parsing

import (
	"bytes"
	"errors"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type Document struct {
	kind         DocumentKind
	Workflow     *definition.Workflow
	Metadata     *actionmeta.Metadata
	SyntaxErrors []*parser.SyntaxError
	// Facts is nil when decoding recovered no semantic input, and an action's Facts are never workflow traversal inputs.
	Facts      *definition.Workflow
	ParseError error
	Parsed     *ParsedSource
}

type ParsedSource struct {
	text     string
	workflow *definition.Workflow
	action   bool
	using    string
}

func (p *ParsedSource) bind(identity string) *workflows.BoundDocument {
	return workflows.Bind(identity, p.text, p.workflow, p.action, p.using)
}

func Workflow(content []byte) Document {
	cloned := bytes.Clone(content)
	w, issues := parser.ParseWorkflow(cloned)
	return Document{
		kind: KindWorkflow, Workflow: w, Facts: w, SyntaxErrors: issues,
		Parsed: &ParsedSource{text: string(cloned), workflow: w},
	}
}

func Action(sourcePath string, content []byte) Document {
	metadata, err := actionmeta.ParseActionMetadata(sourcePath, content)
	document := Document{kind: KindAction, Metadata: metadata, ParseError: err}
	text, parsed := metadata.Text()
	var traversal *definition.Workflow
	_, rejected := errors.AsType[*codec.AdmissionError](err)
	if parsed && !rejected {
		traversal = actionFacts(metadata, text)
	}
	if err != nil {
		document.SyntaxErrors = parser.HandleYAMLUnmarshalError(err)
		for _, issue := range document.SyntaxErrors {
			issue.Position.Line, issue.Position.Column = max(1, issue.Position.Line), max(1, issue.Position.Column)
		}
	} else {
		document.Facts = traversal
	}
	if parsed {
		document.Parsed = &ParsedSource{text: text, workflow: traversal, action: true, using: metadata.Runs.Using}
	}
	return document
}

func (d Document) Kind() DocumentKind { return d.kind }

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

func (d Document) CanAnalyze() bool { return d.Facts != nil }

func actionFacts(metadata *actionmeta.Metadata, text string) *definition.Workflow {
	parser := parser.NewSourceParser([]byte(text))
	parser.IndexScalarSources(metadata.Root)
	job := &definition.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, actionStep(step, parser))
	}
	// Composite output values are evaluated after its steps, like job outputs.
	for _, field := range actionmeta.Validate(metadata).ExpressionFields {
		if field.SchemaPath != "outputs.*.value" || field.IsKey {
			continue
		}
		if job.Outputs == nil {
			job.Outputs = make(map[string]*definition.Output)
		}
		job.Outputs[field.FieldPath] = &definition.Output{Value: &definition.String{Value: field.Value.Value, Pos: new(field.Value.Position)}}
	}
	workflow := &definition.Workflow{Jobs: map[string]*definition.Job{"": job}, ExpressionSources: parser.ExpressionSources(metadata.Root)}
	parser.IndexDependencySpans(workflow)
	return workflow
}

package actionmeta

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func ParseDefinition(content []byte) Definition {
	metadata, err := ParseActionMetadata("action.yml", content)
	if err != nil || metadata == nil || metadata.Root == nil {
		return Definition{}
	}
	return NewDefinition(metadata)
}

type DefinitionInspection struct {
	definition *Definition
	problems   []MetadataProblem
}

func InspectDefinition(content []byte) DefinitionInspection {
	metadata, err := ParseActionMetadata("action.yml", content)
	if err != nil {
		loadDiagnostics := codec.LoadDiagnostics(err)
		problems := make([]MetadataProblem, 0, len(loadDiagnostics))
		for _, item := range loadDiagnostics {
			mark := item.Mark()
			problems = append(problems, MetadataProblem{
				Code: CodeMetadataParse, Path: "action.yml", Message: item.Message(),
				Start: sourcetext.Position{Line: mark.Line, Column: mark.Column},
			})
		}
		if len(problems) == 0 {
			problems = append(problems, MetadataProblem{Code: CodeMetadataParse, Path: "action.yml", Message: err.Error()})
		}
		return DefinitionInspection{problems: problems}
	}
	if metadata == nil || metadata.Root == nil {
		return DefinitionInspection{problems: []MetadataProblem{{Code: CodeMetadataEmpty, Path: "action.yml", Message: "action metadata is empty"}}}
	}
	problems := Validate(metadata).Problems
	if len(problems) != 0 {
		return DefinitionInspection{problems: slices.Clone(problems)}
	}
	definition := NewDefinition(metadata)
	return DefinitionInspection{definition: &definition}
}

func (i DefinitionInspection) Definition() (Definition, bool) {
	if i.definition == nil {
		return Definition{}, false
	}
	return *i.definition, true
}

func (i DefinitionInspection) Problems() []MetadataProblem {
	return slices.Clone(i.problems)
}

type inputContract uint8

const (
	declaredInputs inputContract = iota
	additionalInputs
)

type Definition struct {
	present         bool
	using           string
	inputContract   inputContract
	outputsComplete bool
	inputs          calls.Inputs
	outputs         calls.Outputs
}

func NewDefinition(metadata *Metadata) Definition {
	if metadata == nil {
		return Definition{}
	}
	inputs := maps.Clone(metadata.Inputs)
	outputs := maps.Clone(metadata.Outputs)
	// Only a validated composite action publishes outputs through a fixed mapping.
	complete := metadata.Runs.Using == "composite" && metadata.Runs.Steps != nil && !metadata.SkipOutputs && metadata.Root != nil && len(Validate(metadata).Problems) == 0
	return Definition{present: true, using: metadata.Runs.Using, inputContract: declaredInputs, outputsComplete: complete, inputs: inputs, outputs: outputs}
}

func (d Definition) Present() bool         { return d.present }
func (d Definition) Using() string         { return d.using }
func (d Definition) OutputsComplete() bool { return d.outputsComplete }

func (d Definition) Input(id string) (calls.Input, bool) {
	input, ok := d.inputs[id]
	return input, ok
}

// RequiredInputNames returns the declared spelling of every required input, sorted.
func (d Definition) RequiredInputNames() []string { return d.inputs.RequiredNames() }

func (d Definition) MissingRequiredInputs(supplied func(id string) bool) []calls.Input {
	return d.inputs.MissingRequired(supplied)
}

func (d Definition) AllowsAdditionalInputs() bool { return d.inputContract == additionalInputs }

func (d Definition) WithAdditionalInputs() Definition {
	if d.present {
		d.inputContract = additionalInputs
	}
	return d
}

// InputNames returns the declared spelling of every input, sorted.
func (d Definition) InputNames() []string { return d.inputs.Names() }

// OutputIDs returns the lowercase ID of every output, sorted.
func (d Definition) OutputIDs() []string { return slices.Sorted(maps.Keys(d.outputs)) }

// OutputNames returns the declared spelling of every output, sorted.
func (d Definition) OutputNames() []string { return d.outputs.Names() }

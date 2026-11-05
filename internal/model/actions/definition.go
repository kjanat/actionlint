package action

import (
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

// ParseDefinition extracts call-site facts. Invalid or empty manifests leave
// metadata-dependent checks unavailable; their declarations are not workflow errors.
func ParseDefinition(content []byte) Definition {
	metadata, err := Parse("action.yml", content)
	if err != nil || metadata == nil || metadata.Root == nil {
		return Definition{}
	}
	return NewDefinition(metadata)
}

// DefinitionInspection is an immutable result of inspecting an action manifest.
// A definition is available only when parsing and manifest validation succeed.
type DefinitionInspection struct {
	definition *Definition
	problems   []MetadataProblem
}

// InspectDefinition parses and validates an action manifest before exposing its
// call-site facts. Problems identify locations in the supplied manifest.
func InspectDefinition(content []byte) DefinitionInspection {
	metadata, err := Parse("action.yml", content)
	if err != nil {
		loadDiagnostics := codec.LoadDiagnostics(err)
		problems := make([]MetadataProblem, 0, len(loadDiagnostics))
		for _, item := range loadDiagnostics {
			mark := item.Mark()
			problems = append(problems, MetadataProblem{
				Code: CodeMetadataParse, Path: "action.yml", Message: item.Message(),
				Start: sourcepos.Position{Line: mark.Line, Column: mark.Column},
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
	problems := Validate(metadata)
	if len(problems) != 0 {
		return DefinitionInspection{problems: slices.Clone(problems)}
	}
	definition := NewDefinition(metadata)
	return DefinitionInspection{definition: &definition}
}

// Definition returns the validated call-site definition when the manifest is valid.
func (i DefinitionInspection) Definition() (Definition, bool) {
	if i.definition == nil {
		return Definition{}, false
	}
	return *i.definition, true
}

// Problems returns a detached copy of the manifest's validation problems.
func (i DefinitionInspection) Problems() []MetadataProblem {
	return slices.Clone(i.problems)
}

type inputContract uint8

const (
	declaredInputs inputContract = iota
	additionalInputs
)

// Definition owns the immutable facts used to validate an action call.
// It exposes no manifest syntax, source bytes, or writable metadata pointers.
type Definition struct {
	present       bool
	using         string
	inputContract inputContract
	skipOutputs   bool
	inputs        map[string]Input
	outputs       []string
}

// NewDefinition copies call-site facts from parsed metadata.
func NewDefinition(metadata *Metadata) Definition {
	if metadata == nil {
		return Definition{}
	}
	inputs := make(map[string]Input, len(metadata.Inputs))
	for name, input := range metadata.Inputs {
		if input == nil {
			return Definition{}
		}
		inputs[name] = *input
	}
	outputs := make([]string, 0, len(metadata.Outputs))
	for name := range metadata.Outputs {
		outputs = append(outputs, name)
	}
	slices.Sort(outputs)
	// An omitted declaration does not establish that an action produces no outputs.
	return Definition{present: true, using: metadata.Runs.Using, inputContract: declaredInputs, skipOutputs: metadata.SkipOutputs || metadata.Outputs == nil, inputs: inputs, outputs: outputs}
}

func (d Definition) Present() bool                   { return d.present }
func (d Definition) Using() string                   { return d.using }
func (d Definition) SkipOutputs() bool               { return d.skipOutputs }
func (d Definition) Input(name string) (Input, bool) { input, ok := d.inputs[name]; return input, ok }

// AllowsAdditionalInputs reports implementation-backed support for parameters
// beyond the manifest declarations. Declared inputs retain their requirements.
func (d Definition) AllowsAdditionalInputs() bool { return d.inputContract == additionalInputs }

// WithAdditionalInputs returns a definition with additional parameter support.
// Only reviewed implementation evidence may establish this contract; manifests
// themselves cannot declare it. Declared inputs and outputs are preserved.
func (d Definition) WithAdditionalInputs() Definition {
	if d.present {
		d.inputContract = additionalInputs
	}
	return d
}

func (d Definition) InputNames() []string {
	names := make([]string, 0, len(d.inputs))
	for name := range d.inputs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (d Definition) OutputNames() []string { return slices.Clone(d.outputs) }

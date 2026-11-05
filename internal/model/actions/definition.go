package action

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/results"
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
	definition  *Definition
	diagnostics []results.Diagnostic
}

// InspectDefinition parses and validates an action manifest before exposing its
// call-site facts. Diagnostics retain locations in the supplied manifest.
func InspectDefinition(content []byte) DefinitionInspection {
	metadata, err := Parse("action.yml", content)
	if err != nil {
		loadDiagnostics := codec.LoadDiagnostics(err)
		diagnostics := make([]results.Diagnostic, 0, len(loadDiagnostics))
		for _, item := range loadDiagnostics {
			mark := item.Mark()
			diagnostics = append(diagnostics, results.Diagnostic{
				Rule: "action", Code: string(CodeMetadataParse), Path: "action.yml", Message: item.Message(),
				Start: results.Position{Line: mark.Line, Column: mark.Column},
			})
		}
		if len(diagnostics) == 0 {
			diagnostics = append(diagnostics, results.Diagnostic{Rule: "action", Code: string(CodeMetadataParse), Path: "action.yml", Message: err.Error()})
		}
		return DefinitionInspection{diagnostics: diagnostics}
	}
	if metadata == nil || metadata.Root == nil {
		return DefinitionInspection{diagnostics: []results.Diagnostic{{Rule: "action", Code: string(CodeMetadataEmpty), Path: "action.yml", Message: "action metadata is empty"}}}
	}
	diagnostics := Validate(metadata)
	if len(diagnostics) != 0 {
		return DefinitionInspection{diagnostics: cloneDiagnostics(diagnostics)}
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

// Diagnostics returns an owned copy of manifest diagnostics.
func (i DefinitionInspection) Diagnostics() []results.Diagnostic {
	return cloneDiagnostics(i.diagnostics)
}

func cloneDiagnostics(diagnostics []results.Diagnostic) []results.Diagnostic {
	cloned := slices.Clone(diagnostics)
	for index := range cloned {
		d := &cloned[index]
		if d.Reference != nil {
			reference := *d.Reference
			d.Reference = &reference
		}
		d.Related = slices.Clone(d.Related)
		d.Fixes = slices.Clone(d.Fixes)
		for fixIndex := range d.Fixes {
			d.Fixes[fixIndex].Edits = slices.Clone(d.Fixes[fixIndex].Edits)
			if d.Fixes[fixIndex].Revisions != nil {
				revisions := make(map[string]string, len(d.Fixes[fixIndex].Revisions))
				maps.Copy(revisions, d.Fixes[fixIndex].Revisions)
				d.Fixes[fixIndex].Revisions = revisions
			}
		}
	}
	return cloned
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

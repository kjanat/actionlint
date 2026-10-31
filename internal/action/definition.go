package action

import "slices"

// ParseDefinition extracts call-site facts. Invalid or empty manifests leave
// metadata-dependent checks unavailable; their declarations are not workflow errors.
func ParseDefinition(content []byte) Definition {
	metadata, err := Parse("action.yml", content)
	if err != nil || metadata == nil || metadata.Root == nil {
		return Definition{}
	}
	return NewDefinition(metadata)
}

// Definition owns the immutable facts used to validate an action call.
// It exposes no manifest syntax, source bytes, or writable metadata pointers.
type Definition struct {
	present                 bool
	using                   string
	skipInputs, skipOutputs bool
	inputs                  map[string]Input
	outputs                 []string
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
	return Definition{present: true, using: metadata.Runs.Using, skipInputs: metadata.SkipInputs, skipOutputs: metadata.SkipOutputs, inputs: inputs, outputs: outputs}
}

func (d Definition) Present() bool                   { return d.present }
func (d Definition) Using() string                   { return d.using }
func (d Definition) SkipInputs() bool                { return d.skipInputs }
func (d Definition) SkipOutputs() bool               { return d.skipOutputs }
func (d Definition) Input(name string) (Input, bool) { input, ok := d.inputs[name]; return input, ok }
func (d Definition) InputNames() []string {
	names := make([]string, 0, len(d.inputs))
	for name := range d.inputs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (d Definition) OutputNames() []string { return slices.Clone(d.outputs) }

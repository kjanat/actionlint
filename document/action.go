package document

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/diagnostic"
)

// Action is a lossy projection of selected action declarations.
// ParseStatus describes the metadata decoder's outcome; diagnostics report validity.
// An Action is an immutable value; accessors return detached copies.
type Action struct {
	path, name, description, parseStatus string
	inputs                               []Input
	outputs                              []Output
	runs                                 ActionRuns
}

// ActionFields lists the declarations recorded by NewAction.
type ActionFields struct {
	Path        string
	Name        string
	Description string
	ParseStatus string
	Inputs      []Input
	Outputs     []Output
	Runs        ActionRuns
}

// NewAction records fields; later changes to their storage do not affect it.
func NewAction(fields ActionFields) Action {
	return Action{
		path: fields.Path, name: fields.Name, description: fields.Description, parseStatus: fields.ParseStatus,
		inputs: cloneInputs(fields.Inputs), outputs: cloneOutputs(fields.Outputs), runs: cloneRuns(fields.Runs),
	}
}

// Name returns the declared action name.
func (a Action) Name() string { return a.name }

// Description returns the declared action description.
func (a Action) Description() string { return a.description }

// ParseStatus returns complete, partial or failed.
func (a Action) ParseStatus() string { return a.parseStatus }

// Inputs returns the declared inputs in source order.
func (a Action) Inputs() []Input { return cloneInputs(a.inputs) }

// Outputs returns the declared outputs in source order.
func (a Action) Outputs() []Output { return cloneOutputs(a.outputs) }

// Runs returns the declared execution runtime, or nil when none was decoded.
func (a Action) Runs() ActionRuns { return cloneRuns(a.runs) }

// Input preserves declared required/default values independently.
// A default does not erase an explicitly declared required: true.
type Input struct {
	ID          string               `json:"id"`
	Description string               `json:"description,omitempty"`
	Required    *bool                `json:"required,omitempty"`
	Default     *string              `json:"default,omitempty"`
	Start       *diagnostic.Position `json:"start,omitempty"`
}

// Output preserves an output declaration and optional expression.
type Output struct {
	ID          string               `json:"id"`
	Description string               `json:"description,omitempty"`
	Value       *string              `json:"value,omitempty"`
	Start       *diagnostic.Position `json:"start,omitempty"`
}

func cloneInputs(inputs []Input) []Input {
	if inputs == nil {
		return nil
	}
	out := make([]Input, len(inputs))
	for i, input := range inputs {
		out[i] = Input{
			ID: input.ID, Description: input.Description, Required: cloneBool(input.Required),
			Default: cloneString(input.Default), Start: clonePosition(input.Start),
		}
	}
	return out
}

func cloneOutputs(outputs []Output) []Output {
	if outputs == nil {
		return nil
	}
	out := make([]Output, len(outputs))
	for i, output := range outputs {
		out[i] = Output{ID: output.ID, Description: output.Description, Value: cloneString(output.Value), Start: clonePosition(output.Start)}
	}
	return out
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func (Action) documentOutline() {}

// DocumentPath returns the manifest path.
func (a Action) DocumentPath() string { return a.path }

// WithPath returns a detached copy with a different manifest path.
func (a Action) WithPath(path string) Outline {
	return Action{
		path: path, name: a.name, description: a.description, parseStatus: a.parseStatus,
		inputs: cloneInputs(a.inputs), outputs: cloneOutputs(a.outputs), runs: cloneRuns(a.runs),
	}
}

type actionWire struct {
	Path        string     `json:"path"`
	Name        string     `json:"name,omitempty"`
	Description string     `json:"description,omitempty"`
	ParseStatus string     `json:"parse_status"`
	Inputs      []Input    `json:"inputs"`
	Outputs     []Output   `json:"outputs"`
	Runs        ActionRuns `json:"runs"`
}

// MarshalJSON derives the document kind from the concrete type.
func (a Action) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Kind string `json:"kind"`
		actionWire
	}{"action", actionWire{
		Path: a.path, Name: a.name, Description: a.description, ParseStatus: a.parseStatus,
		Inputs: cloneInputs(a.inputs), Outputs: cloneOutputs(a.outputs), Runs: cloneRuns(a.runs),
	}})
}

// UnmarshalJSON restores the concrete execution-runtime variant.
func (a *Action) UnmarshalJSON(data []byte) error {
	var value actionWire
	wire := struct {
		*actionWire
		Runs json.RawMessage `json:"runs"`
	}{actionWire: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	runs, err := decodeActionRuns(wire.Runs)
	if err != nil {
		return err
	}
	*a = Action{
		path: value.Path, name: value.Name, description: value.Description, parseStatus: value.ParseStatus,
		inputs: value.Inputs, outputs: value.Outputs, runs: runs,
	}
	return nil
}

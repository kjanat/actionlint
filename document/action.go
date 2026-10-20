package document

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/diagnostic"
)

// Action is a lossy projection of selected action declarations.
// ParseStatus describes the metadata decoder's outcome; diagnostics report validity.
type Action struct {
	Path        string     `json:"path"`
	Name        string     `json:"name,omitempty"`
	Description string     `json:"description,omitempty"`
	ParseStatus string     `json:"parse_status"`
	Inputs      []Input    `json:"inputs"`
	Outputs     []Output   `json:"outputs"`
	Runs        ActionRuns `json:"runs"`
}

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

func (Action) documentOutline() {}

// DocumentPath returns the manifest path.
func (a Action) DocumentPath() string { return a.Path }

// WithPath returns a copy with a different manifest path.
func (a Action) WithPath(path string) Outline {
	a.Path = path
	return a
}

// MarshalJSON derives the document kind from the concrete type.
func (a Action) MarshalJSON() ([]byte, error) {
	type plain Action
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"action", plain(a)})
}

// UnmarshalJSON restores the concrete execution-runtime variant.
func (a *Action) UnmarshalJSON(data []byte) error {
	type plain Action
	var value plain
	wire := struct {
		*plain
		Runs json.RawMessage `json:"runs"`
	}{plain: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	runs, err := decodeActionRuns(wire.Runs)
	if err != nil {
		return err
	}
	value.Runs = runs
	*a = Action(value)
	return nil
}

package outline

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/input/source"
)

// Action is a lossy projection of selected action declarations.
// ParseStatus describes the metadata decoder's outcome; diagnostics report validity.
// Runs is nil when no execution runtime was decoded.
type Action struct {
	Path        string                `json:"path"`
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	ParseStatus string                `json:"parse_status"`
	Inputs      []ActionInputOutline  `json:"inputs"`
	Outputs     []ActionOutputOutline `json:"outputs"`
	Runs        ActionRuns            `json:"runs"`
}

// ActionInputOutline preserves declared required/default values independently.
// A default does not erase an explicitly declared required: true.
type ActionInputOutline struct {
	ID          string           `json:"id"`
	Description string           `json:"description,omitempty"`
	Required    *bool            `json:"required,omitempty"`
	Default     *string          `json:"default,omitempty"`
	Start       *source.Position `json:"start,omitempty"`
}

// ActionOutputOutline preserves an output declaration and optional expression.
type ActionOutputOutline struct {
	ID          string           `json:"id"`
	Description string           `json:"description,omitempty"`
	Value       *string          `json:"value,omitempty"`
	Start       *source.Position `json:"start,omitempty"`
}

func (Action) documentOutline() {}

// DocumentPath returns the manifest path.
func (a Action) DocumentPath() string { return a.Path }

// WithPath returns the action with a different manifest path.
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
	var wire struct {
		plain
		Runs json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	runs, err := decodeActionRuns(wire.Runs)
	if err != nil {
		return err
	}
	*a = Action(wire.plain)
	a.Runs = runs
	return nil
}

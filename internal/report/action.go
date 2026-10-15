package report

import "encoding/json"

// ActionOutline is a lossy projection of selected action declarations.
// ParseStatus describes the metadata decoder's outcome; diagnostics report validity.
type ActionOutline struct {
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
	ID          string              `json:"id"`
	Description string              `json:"description,omitempty"`
	Required    *bool               `json:"required,omitempty"`
	Default     *string             `json:"default,omitempty"`
	Start       *DiagnosticPosition `json:"start,omitempty"`
}

// ActionOutputOutline preserves an output declaration and optional expression.
type ActionOutputOutline struct {
	ID          string              `json:"id"`
	Description string              `json:"description,omitempty"`
	Value       *string             `json:"value,omitempty"`
	Start       *DiagnosticPosition `json:"start,omitempty"`
}

func (ActionOutline) documentOutline() {}

// DocumentPath returns the manifest path.
func (a ActionOutline) DocumentPath() string { return a.Path }

// WithPath returns a copy with a different manifest path.
func (a ActionOutline) WithPath(path string) DocumentOutline {
	a.Path = path
	return a
}

// MarshalJSON derives the document kind from the concrete type.
func (a ActionOutline) MarshalJSON() ([]byte, error) {
	type plain ActionOutline
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"action", plain(a)})
}

// UnmarshalJSON restores the concrete execution-runtime variant.
func (a *ActionOutline) UnmarshalJSON(data []byte) error {
	type plain ActionOutline
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
	*a = ActionOutline(value)
	return nil
}

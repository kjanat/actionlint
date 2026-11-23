package outline

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

// Action has a nil Runs when no execution runtime was decoded.
type Action struct {
	Path        string                `json:"path"`
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	ParseStatus string                `json:"parse_status"`
	Inputs      []ActionInputOutline  `json:"inputs"`
	Outputs     []ActionOutputOutline `json:"outputs"`
	Runs        ActionRuns            `json:"runs"`
}

// ActionInputOutline keeps an explicit required: true alongside a default.
type ActionInputOutline struct {
	ID          string               `json:"id"`
	Description string               `json:"description,omitempty"`
	Required    *bool                `json:"required,omitempty"`
	Default     *string              `json:"default,omitempty"`
	Start       *sourcetext.Position `json:"start,omitempty"`
}

type ActionOutputOutline struct {
	ID          string               `json:"id"`
	Description string               `json:"description,omitempty"`
	Value       *string              `json:"value,omitempty"`
	Start       *sourcetext.Position `json:"start,omitempty"`
}

func (Action) documentOutline() {}

func (a Action) DocumentPath() string { return a.Path }

func (a Action) WithPath(path string) Outline {
	a.Path = path
	return a
}

func (a Action) MarshalJSON() ([]byte, error) {
	type plain Action
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"action", plain(a)})
}

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

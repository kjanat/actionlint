package document

import (
	"encoding/json"
	"fmt"
)

// Outline is a lossy report projection of observed workflow or action
// declarations. Its concrete type determines the JSON kind. Diagnostics carry
// validity findings; outlines describe only selected declarations.
type Outline interface {
	documentOutline()
	DocumentPath() string
	WithPath(string) Outline
}

// Outlines restores each document's concrete type when decoding JSON.
type Outlines []Outline

// MarshalJSON emits an empty array when no documents were collected.
func (d Outlines) MarshalJSON() ([]byte, error) {
	type plain Outlines
	if d == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(plain(d))
}

func (Workflow) documentOutline() {}

// DocumentPath returns the source path.
func (w Workflow) DocumentPath() string { return w.Path }

// WithPath returns a copy with a different source path.
func (w Workflow) WithPath(path string) Outline {
	w.Path = path
	return w
}

// MarshalJSON derives the document kind from the concrete type.
func (w Workflow) MarshalJSON() ([]byte, error) {
	type plain Workflow
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"workflow", plain(w)})
}

// UnmarshalJSON dispatches document variants using their kind.
func (d *Outlines) UnmarshalJSON(data []byte) error {
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	values := make(Outlines, 0, len(records))
	for _, record := range records {
		var header struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(record, &header); err != nil {
			return err
		}
		switch header.Kind {
		case "workflow":
			var value Workflow
			if err := json.Unmarshal(record, &value); err != nil {
				return err
			}
			values = append(values, value)
		case "action":
			var value Action
			if err := json.Unmarshal(record, &value); err != nil {
				return err
			}
			values = append(values, value)
		default:
			return fmt.Errorf("unknown document kind %q", header.Kind)
		}
	}
	*d = values
	return nil
}

// ActionRuns identifies one declared action execution runtime.
type ActionRuns interface {
	actionRuns()
}

// CompositeRuns contains the action's declared steps.
type CompositeRuns struct {
	Steps []Step `json:"steps"`
}

// JavaScriptRuns identifies a Node runtime and its entrypoints.
type JavaScriptRuns struct {
	Using  string `json:"using"`
	Main   string `json:"main,omitempty"`
	Pre    string `json:"pre,omitempty"`
	Post   string `json:"post,omitempty"`
	PreIf  string `json:"pre_if,omitempty"`
	PostIf string `json:"post_if,omitempty"`
}

// DockerRuns identifies a container image and its entrypoints.
type DockerRuns struct {
	Image          string   `json:"image,omitempty"`
	Entrypoint     string   `json:"entrypoint,omitempty"`
	PreEntrypoint  string   `json:"pre_entrypoint,omitempty"`
	PostEntrypoint string   `json:"post_entrypoint,omitempty"`
	Args           []string `json:"args"`
}

// PluginRuns identifies a runner-internal plugin entrypoint.
type PluginRuns struct {
	Plugin string `json:"plugin"`
}

// UnknownRuns retains an absent or unrecognized runtime declaration.
type UnknownRuns struct {
	Using string `json:"using,omitempty"`
}

func (CompositeRuns) actionRuns()  {}
func (JavaScriptRuns) actionRuns() {}
func (DockerRuns) actionRuns()     {}
func (PluginRuns) actionRuns()     {}
func (UnknownRuns) actionRuns()    {}

// MarshalJSON derives the runtime kind from the concrete type.
func (r CompositeRuns) MarshalJSON() ([]byte, error) {
	type plain CompositeRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"composite", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r JavaScriptRuns) MarshalJSON() ([]byte, error) {
	type plain JavaScriptRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"javascript", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r DockerRuns) MarshalJSON() ([]byte, error) {
	type plain DockerRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"docker", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r PluginRuns) MarshalJSON() ([]byte, error) {
	type plain PluginRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"plugin", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r UnknownRuns) MarshalJSON() ([]byte, error) {
	type plain UnknownRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"unknown", plain(r)})
}

func decodeActionRuns(data json.RawMessage) (ActionRuns, error) {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Kind {
	case "composite":
		var value CompositeRuns
		err := json.Unmarshal(data, &value)
		return value, err
	case "javascript":
		var value JavaScriptRuns
		err := json.Unmarshal(data, &value)
		return value, err
	case "docker":
		var value DockerRuns
		err := json.Unmarshal(data, &value)
		return value, err
	case "plugin":
		var value PluginRuns
		err := json.Unmarshal(data, &value)
		return value, err
	case "unknown":
		var value UnknownRuns
		err := json.Unmarshal(data, &value)
		return value, err
	default:
		return nil, fmt.Errorf("unknown action runtime kind %q", header.Kind)
	}
}

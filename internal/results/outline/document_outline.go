package outline

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Outline interface {
	documentOutline()
	DocumentPath() string
	WithPath(string) Outline
}

type Outlines []Outline

// MarshalJSON emits an empty array when no documents were collected.
func (d Outlines) MarshalJSON() ([]byte, error) {
	type plain Outlines
	if d == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(plain(d))
}

func (WorkflowOutline) documentOutline() {}

func (w WorkflowOutline) DocumentPath() string { return w.Path }

func (w WorkflowOutline) WithPath(path string) Outline {
	w.Path = path
	return w
}

func (w WorkflowOutline) MarshalJSON() ([]byte, error) {
	type plain WorkflowOutline
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"workflow", plain(w)})
}

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
			type plain WorkflowOutline
			var value plain
			if err := json.Unmarshal(record, &value); err != nil {
				return err
			}
			values = append(values, WorkflowOutline(value))
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

type ActionRuns interface {
	actionRuns()
}

type CompositeRuns struct {
	Steps []StepOutline `json:"steps"`
}

type JavaScriptRuns struct {
	Using  string `json:"using"`
	Main   string `json:"main,omitempty"`
	Pre    string `json:"pre,omitempty"`
	Post   string `json:"post,omitempty"`
	PreIf  string `json:"pre_if,omitempty"`
	PostIf string `json:"post_if,omitempty"`
}

type DockerRuns struct {
	Image          string   `json:"image,omitempty"`
	Entrypoint     string   `json:"entrypoint,omitempty"`
	PreEntrypoint  string   `json:"pre_entrypoint,omitempty"`
	PostEntrypoint string   `json:"post_entrypoint,omitempty"`
	Args           []string `json:"args"`
}

type PluginRuns struct {
	Plugin string `json:"plugin"`
}

type UnknownRuns struct {
	Using string `json:"using,omitempty"`
}

func (CompositeRuns) actionRuns()  {}
func (JavaScriptRuns) actionRuns() {}
func (DockerRuns) actionRuns()     {}
func (PluginRuns) actionRuns()     {}
func (UnknownRuns) actionRuns()    {}

func (r CompositeRuns) MarshalJSON() ([]byte, error) {
	type plain CompositeRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"composite", plain(r)})
}

func (r JavaScriptRuns) MarshalJSON() ([]byte, error) {
	type plain JavaScriptRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"javascript", plain(r)})
}

func (r DockerRuns) MarshalJSON() ([]byte, error) {
	type plain DockerRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"docker", plain(r)})
}

func (r PluginRuns) MarshalJSON() ([]byte, error) {
	type plain PluginRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"plugin", plain(r)})
}

func (r UnknownRuns) MarshalJSON() ([]byte, error) {
	type plain UnknownRuns
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"unknown", plain(r)})
}

func decodeActionRuns(data json.RawMessage) (ActionRuns, error) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	switch header.Kind {
	case "composite":
		return decodeRuns[CompositeRuns](data)
	case "javascript":
		return decodeRuns[JavaScriptRuns](data)
	case "docker":
		return decodeRuns[DockerRuns](data)
	case "plugin":
		return decodeRuns[PluginRuns](data)
	case "unknown":
		return decodeRuns[UnknownRuns](data)
	default:
		return nil, fmt.Errorf("unknown action runtime kind %q", header.Kind)
	}
}

func decodeRuns[T ActionRuns](data json.RawMessage) (ActionRuns, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}

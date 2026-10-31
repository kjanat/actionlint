package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	return marshal(plain(d))
}

func (Workflow) documentOutline() {}

// DocumentPath returns the source path.
func (w Workflow) DocumentPath() string { return w.path }

// WithPath returns a detached copy with a different source path.
func (w Workflow) WithPath(path string) Outline {
	return Workflow{path: path, name: w.name, parseStatus: w.parseStatus, triggers: slices.Clone(w.triggers), jobs: cloneJobs(w.jobs)}
}

// MarshalJSON derives the document kind from the concrete type.
func (w Workflow) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Kind string `json:"kind"`
		workflowWire
	}{"workflow", workflowWire{
		Path: w.path, Name: w.name, ParseStatus: w.parseStatus,
		Triggers: slices.Clone(w.triggers), Jobs: cloneJobs(w.jobs),
	}})
}

// marshal encodes value into storage owned by the caller. A failure is reported
// by message so the error cannot retain the encoded value.
func marshal(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return bytes.Clone(data), nil
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
			var value workflowWire
			if err := json.Unmarshal(record, &value); err != nil {
				return err
			}
			values = append(values, Workflow{
				path: value.Path, name: value.Name, parseStatus: value.ParseStatus,
				triggers: value.Triggers, jobs: value.Jobs,
			})
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
	steps []Step
}

// NewCompositeRuns records steps; later changes to the slice do not affect it.
func NewCompositeRuns(steps []Step) CompositeRuns { return CompositeRuns{steps: cloneSteps(steps)} }

// Steps returns the declared steps in source order.
func (r CompositeRuns) Steps() []Step { return cloneSteps(r.steps) }

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
	image, entrypoint, preEntrypoint, postEntrypoint string
	args                                             []string
}

// DockerRunsFields lists the declarations recorded by NewDockerRuns.
type DockerRunsFields struct {
	Image          string
	Entrypoint     string
	PreEntrypoint  string
	PostEntrypoint string
	Args           []string
}

// NewDockerRuns records fields; later changes to Args do not affect it.
func NewDockerRuns(fields DockerRunsFields) DockerRuns {
	return DockerRuns{
		image: fields.Image, entrypoint: fields.Entrypoint, preEntrypoint: fields.PreEntrypoint,
		postEntrypoint: fields.PostEntrypoint, args: slices.Clone(fields.Args),
	}
}

// Image returns the declared container image.
func (r DockerRuns) Image() string { return r.image }

// Entrypoint returns the declared main entrypoint.
func (r DockerRuns) Entrypoint() string { return r.entrypoint }

// PreEntrypoint returns the declared pre entrypoint.
func (r DockerRuns) PreEntrypoint() string { return r.preEntrypoint }

// PostEntrypoint returns the declared post entrypoint.
func (r DockerRuns) PostEntrypoint() string { return r.postEntrypoint }

// Args returns the declared container arguments.
func (r DockerRuns) Args() []string { return slices.Clone(r.args) }

type compositeRunsWire struct {
	Steps []Step `json:"steps"`
}

type dockerRunsWire struct {
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

// UnknownRuns represents an absent or unrecognized runs.using value.
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
	return marshal(struct {
		Kind string `json:"kind"`
		compositeRunsWire
	}{"composite", compositeRunsWire{Steps: cloneSteps(r.steps)}})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r JavaScriptRuns) MarshalJSON() ([]byte, error) {
	type plain JavaScriptRuns
	return marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"javascript", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r DockerRuns) MarshalJSON() ([]byte, error) {
	return marshal(struct {
		Kind string `json:"kind"`
		dockerRunsWire
	}{"docker", dockerRunsWire{
		Image: r.image, Entrypoint: r.entrypoint, PreEntrypoint: r.preEntrypoint,
		PostEntrypoint: r.postEntrypoint, Args: slices.Clone(r.args),
	}})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r PluginRuns) MarshalJSON() ([]byte, error) {
	type plain PluginRuns
	return marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"plugin", plain(r)})
}

// MarshalJSON derives the runtime kind from the concrete type.
func (r UnknownRuns) MarshalJSON() ([]byte, error) {
	type plain UnknownRuns
	return marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"unknown", plain(r)})
}

// cloneRuns detaches a runtime value from storage shared with its source.
func cloneRuns(runs ActionRuns) ActionRuns {
	switch runs := runs.(type) {
	case CompositeRuns:
		return CompositeRuns{steps: cloneSteps(runs.steps)}
	case DockerRuns:
		return DockerRuns{
			image: runs.image, entrypoint: runs.entrypoint, preEntrypoint: runs.preEntrypoint,
			postEntrypoint: runs.postEntrypoint, args: slices.Clone(runs.args),
		}
	case JavaScriptRuns:
		return JavaScriptRuns{Using: runs.Using, Main: runs.Main, Pre: runs.Pre, Post: runs.Post, PreIf: runs.PreIf, PostIf: runs.PostIf}
	case PluginRuns:
		return PluginRuns{Plugin: runs.Plugin}
	case UnknownRuns:
		return UnknownRuns{Using: runs.Using}
	default:
		return nil
	}
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
		var value compositeRunsWire
		err := json.Unmarshal(data, &value)
		return CompositeRuns{steps: value.Steps}, err
	case "javascript":
		var value JavaScriptRuns
		err := json.Unmarshal(data, &value)
		return value, err
	case "docker":
		var value dockerRunsWire
		err := json.Unmarshal(data, &value)
		return DockerRuns{
			image: value.Image, entrypoint: value.Entrypoint, preEntrypoint: value.PreEntrypoint,
			postEntrypoint: value.PostEntrypoint, args: value.Args,
		}, err
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

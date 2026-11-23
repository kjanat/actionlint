package callreferences

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	workspacePrefix      = "./"
	selfRepositoryPrefix = "$/"
	containerPrefix      = "docker://"
	builtinPrefix        = "builtin:"
	selfHostPrefix       = "self:"
)

type Form uint8

const (
	FormRepository Form = iota
	// FormWorkspace is "./path", relative to the runner workspace.
	FormWorkspace
	// FormSelfRepository is "$/path", relative to the declaring repository revision.
	FormSelfRepository
	FormContainer
	FormBuiltin
)

func FormOf(spec string) Form {
	switch {
	case strings.HasPrefix(spec, workspacePrefix):
		return FormWorkspace
	case strings.HasPrefix(spec, selfRepositoryPrefix):
		return FormSelfRepository
	case strings.HasPrefix(spec, containerPrefix):
		return FormContainer
	case strings.HasPrefix(spec, builtinPrefix):
		return FormBuiltin
	}
	return FormRepository
}

func ContainerImage(spec string) (string, bool) {
	return strings.CutPrefix(spec, containerPrefix)
}

type UsesReference interface {
	usesReference()
}

const (
	HostSourceDefault  = "default"
	HostSourceSelf     = "self"
	HostSourceExplicit = "explicit"
)

type RepositoryReference struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Subpath    string `json:"subpath"`
	Ref        string `json:"ref"`
	Host       string `json:"host,omitempty"`
	Scheme     string `json:"scheme,omitempty"`
	HostSource string `json:"host_source"`
}

type WorkspaceReference struct {
	Path string `json:"path"`
}

type SelfRepositoryReference struct {
	Path string `json:"path"`
}

type ContainerReference struct {
	Image string `json:"image"`
}

type BuiltinReference struct {
	Name string `json:"name"`
}

type UnknownReference struct{}

func (RepositoryReference) usesReference()     {}
func (WorkspaceReference) usesReference()      {}
func (SelfRepositoryReference) usesReference() {}
func (ContainerReference) usesReference()      {}
func (BuiltinReference) usesReference()        {}
func (UnknownReference) usesReference()        {}

func (r RepositoryReference) MarshalJSON() ([]byte, error) {
	type fields RepositoryReference
	return json.Marshal(struct {
		Kind string `json:"kind"`
		fields
	}{"repository", fields(r)})
}

func (r WorkspaceReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}{"workspace", r.Path})
}

func (r SelfRepositoryReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}{"self-repository", r.Path})
}

func (r ContainerReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind  string `json:"kind"`
		Image string `json:"image"`
	}{"container", r.Image})
}

func (r BuiltinReference) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}{"builtin", r.Name})
}

func (UnknownReference) MarshalJSON() ([]byte, error) { return []byte(`{"kind":"unknown"}`), nil }

// Classify parses a uses value without fetching its target.
func Classify(raw string) UsesReference {
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "${{") || strings.TrimSpace(raw) != raw {
		return UnknownReference{}
	}
	switch FormOf(raw) {
	case FormWorkspace:
		return WorkspaceReference{Path: strings.TrimPrefix(raw, workspacePrefix)}
	case FormSelfRepository:
		if strings.Contains(raw, "@") {
			return UnknownReference{}
		}
		return SelfRepositoryReference{Path: strings.TrimLeft(strings.TrimPrefix(raw, selfRepositoryPrefix), "/")}
	case FormContainer:
		if image, _ := ContainerImage(raw); image != "" {
			return ContainerReference{Image: image}
		}
		return UnknownReference{}
	case FormBuiltin:
		if name := strings.TrimPrefix(raw, builtinPrefix); name != "" {
			return BuiltinReference{Name: name}
		}
		return UnknownReference{}
	case FormRepository:
	}
	return classifyRepository(raw)
}

func classifyRepository(raw string) UsesReference {
	r := RepositoryReference{HostSource: HostSourceDefault}
	location := raw
	if rest, self := strings.CutPrefix(location, selfHostPrefix); self {
		r.HostSource, location = HostSourceSelf, rest
	}
	if strings.Contains(location, "://") {
		at := strings.LastIndexByte(location, '@')
		if at >= 0 {
			location, r.Ref = location[:at], location[at+1:]
		}
		u, err := url.Parse(location)
		if err != nil || at >= 0 && r.Ref == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || r.HostSource == HostSourceSelf {
			return UnknownReference{}
		}
		r.Host, r.Scheme, r.HostSource = u.Host, u.Scheme, HostSourceExplicit
		location = strings.TrimPrefix(u.Path, "/")
	} else {
		// The default and self hosts use the runner's action syntax.
		action, problem := ParseRepositoryAction(location)
		if problem != "" {
			return UnknownReference{}
		}
		location, r.Ref = action.Location(), action.Ref
	}
	// Paths the runner accepts but a reference cannot identify, such as "..", stay unknown.
	target, known := ParseRepositoryPath(location)
	if !known {
		return UnknownReference{}
	}
	r.Owner, r.Repo, r.Subpath = target.Owner, target.Repo, target.Subpath
	return r
}

func ClassifyWorkflow(raw string) UsesReference {
	if path, local := strings.CutPrefix(raw, workspacePrefix); local {
		return Classify(selfRepositoryPrefix + path)
	}
	return Classify(raw)
}

func DecodeUsesReference(data json.RawMessage) (UsesReference, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, err
	}
	switch tag.Kind {
	case "repository":
		var r RepositoryReference
		err := json.Unmarshal(data, &r)
		return r, err
	case "workspace":
		var r WorkspaceReference
		err := json.Unmarshal(data, &r)
		return r, err
	case "self-repository":
		var r SelfRepositoryReference
		err := json.Unmarshal(data, &r)
		return r, err
	case "container":
		var r ContainerReference
		err := json.Unmarshal(data, &r)
		return r, err
	case "builtin":
		var r BuiltinReference
		err := json.Unmarshal(data, &r)
		return r, err
	case "unknown":
		return UnknownReference{}, nil
	default:
		return nil, fmt.Errorf("unknown uses reference kind %q", tag.Kind)
	}
}

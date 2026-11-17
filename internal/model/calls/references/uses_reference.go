package references

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Spelling prefixes that select the form of a uses value.
const (
	workspacePrefix      = "./"
	selfRepositoryPrefix = "$/"
	containerPrefix      = "docker://"
	builtinPrefix        = "builtin:"
	selfHostPrefix       = "self:"
)

// Form is the family a uses value belongs to, chosen by its spelling alone.
// It does not check that the rest of the value is well formed; Classify does.
type Form uint8

const (
	// FormRepository is any value without a prefix of another form: owner/repo[/path]@ref
	// and URL spellings, valid or not.
	FormRepository Form = iota
	// FormWorkspace is "./path", relative to the runner workspace.
	FormWorkspace
	// FormSelfRepository is "$/path", relative to the declaring repository revision.
	FormSelfRepository
	// FormContainer is "docker://image".
	FormContainer
	// FormBuiltin is "builtin:name", an action supplied by the runner.
	FormBuiltin
)

// FormOf returns the form spelled by the prefix of a uses value.
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

// ContainerImage returns the image of a "docker://" value. It reports false
// for any other form.
func ContainerImage(spec string) (string, bool) {
	return strings.CutPrefix(spec, containerPrefix)
}

// UsesReference identifies the declared source of an action or reusable workflow.
// Host resolution and release metadata are separate from parsing.
type UsesReference interface {
	usesReference()
}

// Sources of the host a RepositoryReference is read from.
const (
	// HostSourceDefault names the platform's default host.
	HostSourceDefault = "default"
	// HostSourceSelf names the host of the declaring repository.
	HostSourceSelf = "self"
	// HostSourceExplicit names the host written in the reference.
	HostSourceExplicit = "explicit"
)

// RepositoryReference identifies an action or workflow at a repository revision.
// HostSource is default, explicit or self. Default and self require platform context.
type RepositoryReference struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Subpath    string `json:"subpath"`
	Ref        string `json:"ref"`
	Host       string `json:"host,omitempty"`
	Scheme     string `json:"scheme,omitempty"`
	HostSource string `json:"host_source"`
}

// WorkspaceReference identifies an action in the runner workspace.
type WorkspaceReference struct {
	Path string `json:"path"`
}

// SelfRepositoryReference identifies a path in the declaring repository revision.
type SelfRepositoryReference struct {
	Path string `json:"path"`
}

// ContainerReference identifies a container image by name, tag or digest.
type ContainerReference struct {
	Image string `json:"image"`
}

// BuiltinReference identifies an action supplied by the runner.
type BuiltinReference struct {
	Name string `json:"name"`
}

// UnknownReference preserves an unclassified uses value in the enclosing declaration.
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

// Classify parses a uses value without fetching its target. An empty value has
// no reference. Classification does not establish support in the selected
// workflow dialect.
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
	if at := strings.LastIndexByte(location, '@'); at >= 0 {
		location, r.Ref = location[:at], location[at+1:]
		if r.Ref == "" {
			return UnknownReference{}
		}
	}
	if strings.Contains(location, "://") {
		u, err := url.Parse(location)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || r.HostSource == HostSourceSelf {
			return UnknownReference{}
		}
		r.Host, r.Scheme, r.HostSource = u.Host, u.Scheme, HostSourceExplicit
		location = strings.TrimPrefix(u.Path, "/")
	} else if r.Ref == "" {
		return UnknownReference{}
	}
	target, known := ParseRepositoryPath(location)
	if !known {
		return UnknownReference{}
	}
	r.Owner, r.Repo, r.Subpath = target.Owner, target.Repo, target.Subpath
	return r
}

// ClassifyWorkflow parses a job-level uses value.
// GitHub resolves a local reusable workflow from the caller's commit, so ./ and $/ calls are both self-repository references.
func ClassifyWorkflow(raw string) UsesReference {
	if path, local := strings.CutPrefix(raw, workspacePrefix); local {
		return Classify(selfRepositoryPrefix + path)
	}
	return Classify(raw)
}

// DecodeUsesReference reads a reference written by json.Marshal. Null and empty
// input decode to no reference.
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

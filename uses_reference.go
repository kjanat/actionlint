package actionlint

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// UsesReference identifies the declared source of an action or reusable workflow.
// Host resolution and release metadata are separate from parsing.
type UsesReference interface {
	usesReference()
}

// RepositoryReference retains a repository, optional subpath and Git ref.
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

// ContainerReference retains the image reference, including any tag or digest.
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

// ParseUsesReference classifies a declaration without fetching its target.
// Classification does not establish support in the selected workflow dialect.
func ParseUsesReference(raw string) UsesReference {
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "${{") || strings.TrimSpace(raw) != raw {
		return UnknownReference{}
	}
	switch {
	case strings.HasPrefix(raw, "./"):
		return WorkspaceReference{Path: strings.TrimPrefix(raw, "./")}
	case strings.HasPrefix(raw, "$/"):
		if strings.Contains(raw, "@") {
			return UnknownReference{}
		}
		return SelfRepositoryReference{Path: strings.TrimLeft(strings.TrimPrefix(raw, "$/"), "/")}
	case strings.HasPrefix(raw, "docker://"):
		if image := strings.TrimPrefix(raw, "docker://"); image != "" {
			return ContainerReference{Image: image}
		}
		return UnknownReference{}
	case strings.HasPrefix(raw, "builtin:"):
		if name := strings.TrimPrefix(raw, "builtin:"); name != "" {
			return BuiltinReference{Name: name}
		}
		return UnknownReference{}
	}
	r := RepositoryReference{HostSource: "default"}
	location := raw
	if rest, self := strings.CutPrefix(location, "self:"); self {
		r.HostSource, location = "self", rest
	}
	if at := strings.LastIndexByte(location, '@'); at >= 0 {
		location, r.Ref = location[:at], location[at+1:]
		if r.Ref == "" {
			return UnknownReference{}
		}
	}
	if strings.Contains(location, "://") {
		u, err := url.Parse(location)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || r.HostSource == "self" {
			return UnknownReference{}
		}
		r.Host, r.Scheme, r.HostSource = u.Host, u.Scheme, "explicit"
		location = strings.TrimPrefix(u.Path, "/")
	} else if r.Ref == "" {
		return UnknownReference{}
	}
	parts := strings.Split(location, "/")
	if len(parts) < 2 || strings.ContainsAny(location, "@: \\\t\r\n") {
		return UnknownReference{}
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return UnknownReference{}
		}
	}
	r.Owner, r.Repo, r.Subpath = parts[0], parts[1], strings.Join(parts[2:], "/")
	return r
}

func decodeUsesReference(data json.RawMessage) (UsesReference, error) {
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

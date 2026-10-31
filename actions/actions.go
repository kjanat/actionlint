// Package actions supplies remote action manifests without selecting a network client.
package actions

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/knowledge"
)

// Reference identifies the requested action, before a mutable ref is resolved.
type Reference struct{ host, repository, directory, ref string }

// ParseReference validates a remote uses reference for one host.
func ParseReference(host, spec string) (Reference, error) {
	u, err := url.Parse("https://" + host)
	if err != nil || u.Host != host || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || host == "" {
		return Reference{}, fmt.Errorf("invalid action host %q", host)
	}
	location, ref, ok := strings.Cut(spec, "@")
	if !ok || ref == "" {
		return Reference{}, errors.New("action ref is missing")
	}
	parts := strings.SplitN(location, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == "$" {
		return Reference{}, errors.New("action needs an owner and repository")
	}
	directory := ""
	if len(parts) == 3 {
		directory = parts[2]
	}
	if directory != "" && (path.Clean(directory) != directory || strings.HasPrefix(directory, "../") || directory == ".." || strings.HasPrefix(directory, "/") || strings.Contains(directory, "\\")) {
		return Reference{}, fmt.Errorf("invalid action directory %q", directory)
	}
	return Reference{host: strings.ToLower(host), repository: parts[0] + "/" + parts[1], directory: directory, ref: ref}, nil
}

func (r Reference) Host() string       { return r.host }
func (r Reference) Repository() string { return r.repository }
func (r Reference) Directory() string  { return r.directory }
func (r Reference) Ref() string        { return r.ref }

// Identity binds a manifest to an immutable repository commit and action path.
// This identifies fetched content; it does not describe how a ref selected it.
type Identity struct {
	host, repository, commit, directory string
	repositoryID                        uint64
}

func NewIdentity(host, repository string, repositoryID uint64, commit, directory string) (Identity, error) {
	location := repository
	if directory != "" {
		location += "/" + directory
	}
	reference, err := ParseReference(host, location+"@"+commit)
	if err != nil {
		return Identity{}, err
	}
	if reference.Repository() != repository || reference.Directory() != directory {
		return Identity{}, errors.New("invalid action repository or directory")
	}
	if repositoryID == 0 {
		return Identity{}, errors.New("action repository ID is missing")
	}
	if len(commit) != 40 && len(commit) != 64 {
		return Identity{}, errors.New("action commit must be a full object ID")
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return Identity{}, fmt.Errorf("invalid action commit: %w", err)
	}
	return Identity{host: reference.host, repository: reference.repository, repositoryID: repositoryID, commit: strings.ToLower(commit), directory: reference.directory}, nil
}

func (i Identity) Host() string         { return i.host }
func (i Identity) Repository() string   { return i.repository }
func (i Identity) RepositoryID() uint64 { return i.repositoryID }
func (i Identity) Commit() string       { return i.commit }
func (i Identity) Directory() string    { return i.directory }

// Route records whether selection used a raw object ID or an exact lookup key.
type Route struct {
	object    bool
	reference string
}

func ObjectRoute() Route { return Route{object: true} }
func RefRoute(reference string) (Route, error) {
	if reference != "HEAD" && (!strings.HasPrefix(reference, "refs/") || reference == "refs/") {
		return Route{}, errors.New("selection route needs an exact ref lookup key")
	}
	return Route{reference: reference}, nil
}
func (r Route) Object() bool      { return r.object }
func (r Route) Reference() string { return r.reference }

// Selection records provider evidence for one action request. The original
// selected object survives tag peeling; Identity identifies terminal content.
// Observation labels are not freshness guarantees. Construction validates shape
// and consistency, not the authenticity or completeness of supplied evidence.
type Selection struct {
	request    Reference
	identity   Identity
	object     string
	route      Route
	observedAt string
}

func NewSelection(request Reference, identity Identity, object string, route Route, observedAt string) (Selection, error) {
	if request.host == "" || request.ref == "" || identity.commit == "" || request.host != identity.host || !strings.EqualFold(request.repository, identity.repository) || request.directory != identity.directory {
		return Selection{}, errors.New("action selection does not belong to its request")
	}
	if len(object) != 40 && len(object) != 64 {
		return Selection{}, errors.New("selected action object must be a full object ID")
	}
	if _, err := hex.DecodeString(object); err != nil {
		return Selection{}, fmt.Errorf("invalid selected action object: %w", err)
	}
	if !route.object && route.reference == "" {
		return Selection{}, errors.New("action selection route is missing")
	}
	if route.object && !strings.EqualFold(request.ref, object) {
		return Selection{}, errors.New("selected object does not match the raw object request")
	}
	return Selection{request: request, identity: identity, object: strings.ToLower(object), route: route, observedAt: observedAt}, nil
}

func (s Selection) Request() Reference { return s.request }
func (s Selection) Identity() Identity { return s.identity }
func (s Selection) Object() string     { return s.object }
func (s Selection) Route() Route       { return s.route }
func (s Selection) ObservedAt() string { return s.observedAt }

// Manifest owns bytes obtained for a provider selection. Analysis parses the
// same manifest for input, runtime and output checks. This is not proof that a
// runner executed these bytes or that their transitive dependencies are pinned.
type Manifest struct {
	selection Selection
	content   string
	missing   knowledge.Unavailability
}

func NewManifest(selection Selection, content []byte) (Manifest, error) {
	if selection.identity.commit == "" {
		return Manifest{}, errors.New("action manifest has no provider selection")
	}
	if len(content) == 0 {
		return Manifest{}, errors.New("action manifest is empty")
	}
	return Manifest{selection: selection, content: string(content)}, nil
}
func (m Manifest) Identity() Identity      { return m.selection.identity }
func (m Manifest) Selection() Selection    { return m.selection }
func (m Manifest) Content() string         { return m.content }
func (m Manifest) Available() bool         { return m.selection.identity.commit != "" }
func (m Manifest) Value() (Manifest, bool) { return m, m.Available() }
func (m Manifest) Reason() knowledge.Reason {
	if m.Available() {
		return ""
	}
	return m.missing.Reason()
}
func UnavailableManifest(reason knowledge.Unavailability) Manifest { return Manifest{missing: reason} }

// Provider resolves a reference and obtains its manifest. Expected transport
// failures return unavailable knowledge; cancellation is returned as an error.
// Calls may be concurrent, and implementations must honor ctx.
type Provider interface {
	Lookup(context.Context, Reference) (Manifest, error)
}

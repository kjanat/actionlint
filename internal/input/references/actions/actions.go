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

	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Reference identifies the requested action, before a mutable ref is resolved.
type Reference struct{ host, repository, directory, ref string }

// ParseReference validates a remote uses reference for one host.
func ParseReference(host, spec string) (Reference, error) {
	u, err := url.Parse("https://" + host)
	if err != nil || u.Host != host || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || host == "" {
		return Reference{}, fmt.Errorf("invalid action host %q", host)
	}
	if _, ref, _ := strings.Cut(spec, "@"); ref == "" {
		return Reference{}, errors.New("action ref is missing")
	}
	if strings.Count(spec, "@") > 1 {
		return Reference{}, errors.New(`action reference contains more than one "@"`)
	}
	if strings.Count(spec, "@") > 1 {
		return Reference{}, errors.New(`action reference contains more than one "@"`)
	}
	target, problem := callreferences.ParseRepositoryAction(spec)
	if problem != "" || isLocalForm(spec) {
		return Reference{}, errors.New("action needs an owner and repository")
	}
	directory := target.Subpath
	if directory != "" && (path.Clean(directory) != directory || strings.HasPrefix(directory, "../") || directory == "..") {
		return Reference{}, fmt.Errorf("invalid action directory %q", directory)
	}
	return Reference{host: strings.ToLower(host), repository: target.Owner + "/" + target.Repo, directory: directory, ref: target.Ref}, nil
}

// isLocalForm reports whether spec spells a workspace or self-repository path,
// which has no owner.
func isLocalForm(spec string) bool {
	form := callreferences.FormOf(spec)
	return form == callreferences.FormWorkspace || form == callreferences.FormSelfRepository
}

func (r Reference) Host() string       { return r.host }
func (r Reference) Repository() string { return r.repository }
func (r Reference) Directory() string  { return r.directory }
func (r Reference) Ref() string        { return r.ref }

// Identity binds a manifest to an immutable repository commit and action path.
// This identifies fetched content; it does not describe how a ref selected it.
type Identity struct {
	host, repository, commit, directory string
	repositoryID                        repository.ID
}

func NewIdentity(host, repository string, repositoryID repository.ID, commit, directory string) (Identity, error) {
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
	if len(commit) != 40 && len(commit) != 64 {
		return Identity{}, errors.New("action commit must be a full object ID")
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return Identity{}, fmt.Errorf("invalid action commit: %w", err)
	}
	return Identity{host: reference.host, repository: reference.repository, repositoryID: repositoryID, commit: strings.ToLower(commit), directory: reference.directory}, nil
}

func (i Identity) Host() string                { return i.host }
func (i Identity) Repository() string          { return i.repository }
func (i Identity) RepositoryID() repository.ID { return i.repositoryID }
func (i Identity) Commit() string              { return i.commit }
func (i Identity) Directory() string           { return i.directory }

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

// ReferenceSelection records provider evidence for one action request. The original
// selected object survives tag peeling; Identity identifies terminal content.
// Observation labels are not freshness guarantees. Construction validates shape
// and consistency, not the authenticity or completeness of supplied evidence.
type ReferenceSelection struct {
	request    Reference
	identity   Identity
	object     string
	route      Route
	observedAt string
}

func NewReferenceSelection(request Reference, identity Identity, object string, route Route, observedAt string) (ReferenceSelection, error) {
	if request.host == "" || request.ref == "" || identity.commit == "" || request.host != identity.host || !strings.EqualFold(request.repository, identity.repository) || request.directory != identity.directory {
		return ReferenceSelection{}, errors.New("action selection does not belong to its request")
	}
	if len(object) != 40 && len(object) != 64 {
		return ReferenceSelection{}, errors.New("selected action object must be a full object ID")
	}
	if _, err := hex.DecodeString(object); err != nil {
		return ReferenceSelection{}, fmt.Errorf("invalid selected action object: %w", err)
	}
	if !route.object && route.reference == "" {
		return ReferenceSelection{}, errors.New("action selection route is missing")
	}
	if route.object && !strings.EqualFold(request.ref, object) {
		return ReferenceSelection{}, errors.New("selected object does not match the raw object request")
	}
	return ReferenceSelection{request: request, identity: identity, object: strings.ToLower(object), route: route, observedAt: observedAt}, nil
}

func (s ReferenceSelection) Request() Reference { return s.request }
func (s ReferenceSelection) Identity() Identity { return s.identity }
func (s ReferenceSelection) Object() string     { return s.object }
func (s ReferenceSelection) Route() Route       { return s.route }
func (s ReferenceSelection) ObservedAt() string { return s.observedAt }

// Manifest owns bytes obtained for a provider selection. Analysis parses the
// same manifest for input, runtime and output checks. This is not proof that a
// runner executed these bytes or that their transitive dependencies are pinned.
type Manifest struct {
	selection ReferenceSelection
	content   string
	filename  string
	missing   availability.Unavailability
}

func NewManifest(selection ReferenceSelection, content []byte) (Manifest, error) {
	return NewManifestFile(selection, "action.yml", content)
}

// NewManifestFile records the selected metadata filename and contents,
// including empty files. Analysis classifies empty metadata files as invalid.
func NewManifestFile(selection ReferenceSelection, filename string, content []byte) (Manifest, error) {
	if selection.identity.commit == "" {
		return Manifest{}, errors.New("action manifest has no provider selection")
	}
	if filename != "action.yml" && filename != "action.yaml" {
		return Manifest{}, errors.New("invalid action manifest filename")
	}
	return Manifest{selection: selection, content: string(content), filename: filename}, nil
}
func (m Manifest) Filename() string { return m.filename }
func (m Manifest) URL() string {
	if !m.Available() {
		return ""
	}
	i := m.Identity()
	u := url.URL{Scheme: "https", Host: i.Host(), Path: path.Join(i.Repository(), "blob", i.Commit(), i.Directory(), m.Filename())}
	return u.String()
}
func (m Manifest) Identity() Identity            { return m.selection.identity }
func (m Manifest) Selection() ReferenceSelection { return m.selection }
func (m Manifest) Content() string               { return m.content }
func (m Manifest) Available() bool               { return m.selection.identity.commit != "" }
func (m Manifest) Value() (Manifest, bool)       { return m, m.Available() }
func (m Manifest) Unavailability() (availability.Unavailability, bool) {
	return m.missing, !m.Available()
}

func (m Manifest) Reason() availability.Reason {
	if m.Available() {
		return ""
	}
	return m.missing.Reason()
}

func UnavailableManifest(reason availability.Unavailability) Manifest {
	return Manifest{missing: reason}
}

// Provider resolves a reference and obtains its manifest. Expected transport
// failures return unavailable knowledge; cancellation is returned as an error.
// Calls may be concurrent, and implementations must honor ctx.
type Provider interface {
	Lookup(context.Context, Reference) (Manifest, error)
}

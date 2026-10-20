// Package pluginref defines plugin dependency declarations independently of
// registry transport, artifact resolution, and runtime loading.
package pluginref

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// DefaultRegistry is the public actionlint plugin catalogue.
const DefaultRegistry = "https://plugins.actionlint.kjanat.dev/"

var (
	repositoryPattern = regexp.MustCompile(`^(github\.com|gitlab\.com)/[a-z0-9][a-z0-9_-]*(/[a-z0-9][a-z0-9._-]*)+$`)
	versionPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
)

// Reference declares a repository, requested release, and catalogue.
// Version is "latest" or an exact release tag, never a resolved artifact.
type Reference struct {
	Name     string
	Version  string
	Registry string
}

// Parse accepts owner/repo or a supported forge-qualified repository, optionally
// followed by @latest or an exact release tag.
func Parse(value string) (Reference, error) {
	name, version, found := strings.Cut(value, "@")
	if found && version == "" {
		return Reference{}, errors.New("plugin version must not be empty after @")
	}
	return New(name, version, "")
}

// New validates and normalizes a plugin declaration. Empty version and registry
// select latest stable and DefaultRegistry respectively.
func New(name, version, registry string) (Reference, error) {
	name = strings.ToLower(name)
	if strings.Count(name, "/") == 1 {
		name = "github.com/" + name
	}
	if len(name) > 523 || !repositoryPattern.MatchString(name) ||
		(strings.HasPrefix(name, "github.com/") && strings.Count(name, "/") != 2) {
		return Reference{}, fmt.Errorf("invalid plugin repository %q: use owner/repo, github.com/owner/repo, or gitlab.com/group/repo", name)
	}
	if version == "" {
		version = "latest"
	}
	if !versionPattern.MatchString(version) {
		return Reference{}, fmt.Errorf("invalid plugin version %q: use latest or an exact release tag", version)
	}
	if registry == "" {
		registry = DefaultRegistry
	}
	u, err := url.Parse(registry)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		strings.Contains(u.Path, "\\") || strings.Contains(registry, "#") {
		return Reference{}, errors.New("plugin registry must be an HTTPS directory URL without credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(u.Path, "/")), "/") + "/"
	u.RawPath = ""
	return Reference{Name: name, Version: version, Registry: u.String()}, nil
}

// String returns the canonical repository with an exact tag when requested.
func (r Reference) String() string {
	if r.Version == "latest" {
		return r.Name
	}
	return r.Name + "@" + r.Version
}

// Package pluginref defines plugin dependency declarations independently of
// registry transport, artifact resolution, and runtime loading.
package pluginref

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// DefaultRegistry is the public actionlint plugin catalogue.
const DefaultRegistry = "https://plugins.actionlint.kjanat.dev/"

var (
	shortNamePattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	githubRepositoryPattern = regexp.MustCompile(`^github\.com/[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9._-][A-Za-z0-9._-]*$`)
	gitlabSegmentPattern    = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)
	versionPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*(/[A-Za-z0-9][A-Za-z0-9._+-]*)*$`)
)

// ValidVersion accepts exact release tags, including slash-separated tag names.
func ValidVersion(version string) bool {
	if len(version) > 128 || !versionPattern.MatchString(version) || strings.Contains(version, "..") || strings.HasSuffix(version, ".") {
		return false
	}
	for segment := range strings.SplitSeq(version, "/") {
		if strings.HasSuffix(segment, ".lock") {
			return false
		}
	}
	return true
}

// ValidRepository validates forge-qualified repository paths for declarations
// and catalogue identities.
func ValidRepository(name string) bool {
	if len(name) > 523 {
		return false
	}
	if strings.HasPrefix(name, "github.com/") {
		if !githubRepositoryPattern.MatchString(name) {
			return false
		}
		repository := name[strings.LastIndexByte(name, '/')+1:]
		return repository != "." && repository != ".."
	}
	pathname, ok := strings.CutPrefix(name, "gitlab.com/")
	if !ok || !strings.Contains(pathname, "/") {
		return false
	}
	segments := strings.Split(pathname, "/")
	for i, segment := range segments {
		lower := strings.ToLower(segment)
		if !gitlabSegmentPattern.MatchString(segment) || segment == "." || segment == ".." ||
			strings.HasSuffix(lower, ".git") || strings.HasSuffix(lower, ".atom") {
			return false
		}
		// GitLab namespaces cannot end in a dot; project paths may.
		if i < len(segments)-1 && strings.HasSuffix(segment, ".") {
			return false
		}
	}
	return true
}

// Reference declares a registry alias or repository, requested release, and catalogue.
// Version is "latest" or an exact release tag, never a resolved artifact.
type Reference struct {
	Name     string
	Version  string
	Registry string
	direct   string
}

// Parse accepts a registry alias, owner/repo or a supported forge-qualified
// repository, optionally followed by @latest or an exact release tag.
func Parse(value string) (Reference, error) {
	if strings.Contains(value, "://") {
		return directURL(value)
	}
	name, version, found := strings.Cut(value, "@")
	if found && version == "" {
		return Reference{}, errors.New("plugin version must not be empty after @")
	}
	return New(name, version, "")
}

// DirectSource returns a SHA-pinned artifact URL, or empty for registry entries.
// Direct declarations already contain their immutable pin and need no lock row.
func (r Reference) DirectSource() string { return r.direct }

// IsAlias reports a registry-assigned shorthand, whose repository is resolved
// by that registry and recorded in the project lock.
func (r Reference) IsAlias() bool { return r.direct == "" && ValidAlias(r.Name) }

// ValidAlias checks the shared catalogue and declaration shorthand syntax.
func ValidAlias(name string) bool { return shortNamePattern.MatchString(name) }

// Equal compares normalized declaration identities and selection intent.
func (r Reference) Equal(other Reference) bool { return r == other }

// Normalize validates either supported declaration form.
func (r Reference) Normalize() (Reference, error) {
	if r.direct != "" {
		return directURL(r.direct)
	}
	return New(r.Name, r.Version, r.Registry)
}

func directURL(value string) (Reference, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return Reference{}, errors.New("direct plugin URL must use HTTPS without credentials")
	}
	digest, ok := strings.CutPrefix(u.Fragment, "sha256=")
	decoded, err := hex.DecodeString(digest)
	if !ok || err != nil || len(decoded) != 32 {
		return Reference{}, errors.New("direct plugin URL requires #sha256=<64 hex digits>")
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment, u.RawFragment = "", ""
	name := u.String()
	u.Fragment = "sha256=" + strings.ToLower(digest)
	return Reference{Name: name, direct: u.String()}, nil
}

// New validates and normalizes a plugin declaration. Empty version and registry
// select latest stable and DefaultRegistry respectively.
func New(name, version, registry string) (Reference, error) {
	name = strings.ToLower(name)
	if strings.Count(name, "/") == 1 {
		name = "github.com/" + name
	}
	if !ValidAlias(name) && !ValidRepository(name) {
		return Reference{}, fmt.Errorf("invalid plugin reference %q: use a registry alias, owner/repo, github.com/owner/repo, or gitlab.com/group/repo", name)
	}
	if version == "" {
		version = "latest"
	}
	if !ValidVersion(version) {
		return Reference{}, fmt.Errorf("invalid plugin version %q: use latest or an exact release tag", version)
	}
	if registry == "" {
		registry = DefaultRegistry
	}
	u, err := url.Parse(registry)
	if err == nil && u.Scheme == "file" {
		if _, err := LocalRegistryDirectory(registry); err != nil {
			return Reference{}, err
		}
		u.Path = strings.TrimSuffix(path.Clean(u.Path), "/") + "/"
		u.RawPath = ""
		return Reference{Name: name, Version: version, Registry: u.String()}, nil
	}
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		strings.Contains(u.Path, "\\") || strings.Contains(registry, "#") {
		return Reference{}, errors.New("plugin registry must be an HTTPS directory URL without credentials, query or fragment")
	}
	host, zone, scoped := strings.Cut(u.Host, "%")
	u.Host = strings.ToLower(host)
	if scoped {
		u.Host += "%" + zone
	}
	u.Path = strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(u.Path, "/")), "/") + "/"
	u.RawPath = ""
	return Reference{Name: name, Version: version, Registry: u.String()}, nil
}

// LocalRegistryDirectory accepts an explicit absolute file URI. Local candidate
// catalogues have no authenticated publisher and cannot substitute for HTTPS registries.
func LocalRegistryDirectory(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || u.Host != "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || strings.Contains(u.Path, "\\") || strings.HasPrefix(u.Path, "//") {
		return "", errors.New("local registry must be an absolute file URI without host, query or fragment")
	}
	name := u.Path
	if runtime.GOOS == "windows" && len(name) >= 3 && name[0] == '/' && name[2] == ':' {
		name = name[1:]
	}
	name = filepath.FromSlash(name)
	if !filepath.IsAbs(name) {
		return "", errors.New("local registry requires an absolute directory")
	}
	return filepath.Clean(name), nil
}

// String preserves the declared alias or full repository identity.
func (r Reference) String() string {
	if r.direct != "" {
		return r.direct
	}
	name := r.Name
	if r.Version == "latest" {
		return name
	}
	return name + "@" + r.Version
}

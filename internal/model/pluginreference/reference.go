// Package pluginreference defines plugin dependency declarations independently of
// registry transport, artifact resolution, and runtime loading.
package pluginreference

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/endpoint"
)

const DefaultRegistry = "https://plugins.actionlint.kjanat.dev/"

var (
	shortNamePattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	githubRepositoryPattern = regexp.MustCompile(`^github\.com/[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9._-][A-Za-z0-9._-]*$`)
	gitlabSegmentPattern    = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)
	versionPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*(/[A-Za-z0-9][A-Za-z0-9._+-]*)*$`)
)

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

func ValidName(name string) bool {
	if ValidRepository(name) {
		return true
	}
	parts := strings.Split(name, "/")
	if len(parts) < 4 || parts[0] != "github.com" || !ValidRepository(strings.Join(parts[:3], "/")) || len(name) > 523 {
		return false
	}
	for _, part := range parts[3:] {
		if !gitlabSegmentPattern.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return true
}

type Reference struct {
	name            string
	version         string
	registry        string
	direct          string
	candidateDigest string
}

func (r Reference) Name() string { return r.name }

func (r Reference) Version() string { return r.version }

func (r Reference) Registry() string { return r.registry }

func (r Reference) CandidateDigest() string { return r.candidateDigest }

func ParsePluginReference(value string) (Reference, error) {
	if strings.Contains(value, "://") {
		return directURL(value)
	}
	name, version, found := strings.Cut(value, "@")
	if found && version == "" {
		return Reference{}, errors.New("plugin version must not be empty after @")
	}
	return New(name, version, "")
}

// DirectSource returns a SHA-pinned artifact URL or oci:// digest reference,
// or empty for registry entries.
func (r Reference) DirectSource() string { return r.direct }

func (r Reference) IsAlias() bool { return r.direct == "" && ValidAlias(r.name) }

func ValidAlias(name string) bool { return shortNamePattern.MatchString(name) }

func (r Reference) Equal(other Reference) bool { return r == other }

func (r Reference) Normalize() (Reference, error) {
	if r.direct != "" {
		return directURL(r.direct)
	}
	if r.candidateDigest != "" {
		return NewCandidate(r.name, r.version, r.registry, r.candidateDigest)
	}
	return New(r.name, r.version, r.registry)
}

func NewCandidate(name, version, registry, digest string) (Reference, error) {
	ref, err := New(name, version, registry)
	if err != nil {
		return Reference{}, err
	}
	if ref.IsAlias() || !ValidName(ref.Name()) {
		return Reference{}, errors.New("candidate digest requires a canonical plugin repository")
	}
	if ref.Version() == "latest" {
		return Reference{}, errors.New("candidate digest requires an exact plugin version")
	}
	if _, err := LocalRegistryDirectory(ref.Registry()); err != nil {
		return Reference{}, errors.New("candidate digest requires a local file registry")
	}
	normalized, ok := NormalizeDigest(digest)
	if !ok {
		return Reference{}, errors.New("candidate digest must be 64 hexadecimal SHA256 digits")
	}
	ref.candidateDigest = normalized
	return ref, nil
}

func directURL(value string) (Reference, error) {
	if IsOCI(value) {
		return directOCI(value)
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return Reference{}, errors.New("direct plugin URL must use HTTPS without credentials")
	}
	digest, prefixed := strings.CutPrefix(u.Fragment, "sha256=")
	digest, ok := NormalizeDigest(digest)
	if !prefixed || !ok {
		return Reference{}, errors.New("direct plugin URL requires #sha256=<64 hex digits>")
	}
	authority, err := endpoint.Of(u)
	if err != nil {
		return Reference{}, errors.New("direct plugin URL must use HTTPS without credentials")
	}
	u.Host = authority.URLHost()
	u.Fragment, u.RawFragment = "", ""
	name := u.String()
	u.Fragment = "sha256=" + digest
	return Reference{name: name, direct: u.String()}, nil
}

func directOCI(value string) (Reference, error) {
	ref, err := ParseOCI(value)
	if err != nil {
		return Reference{}, err
	}
	name := "oci://" + ref.Host() + "/" + ref.Repository()
	return Reference{name: name, direct: ref.String()}, nil
}

func New(name, version, registry string) (Reference, error) {
	name = strings.ToLower(name)
	if strings.Count(name, "/") == 1 {
		name = "github.com/" + name
	}
	if !ValidAlias(name) && !ValidName(name) {
		return Reference{}, fmt.Errorf("invalid plugin reference %q: use a registry alias, owner/repo, or a forge-qualified repository/package", name)
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
		return Reference{name: name, version: version, registry: u.String()}, nil
	}
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		strings.Contains(u.Path, "\\") || strings.Contains(registry, "#") {
		return Reference{}, errors.New("plugin registry must be an HTTPS directory URL without credentials, query or fragment")
	}
	authority, err := endpoint.Of(u)
	if err != nil {
		return Reference{}, errors.New("plugin registry must be an HTTPS directory URL without credentials, query or fragment")
	}
	u.Host = authority.URLHost()
	u.Path = strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(u.Path, "/")), "/") + "/"
	u.RawPath = ""
	return Reference{name: name, version: version, registry: u.String()}, nil
}

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

func (r Reference) String() string {
	if r.direct != "" {
		return r.direct
	}
	name := r.name
	if r.version == "latest" {
		return name
	}
	return name + "@" + r.version
}

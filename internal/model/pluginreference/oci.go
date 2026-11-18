package pluginreference

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/endpoint"
)

var (
	ociHostPattern       = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)
	ociRepositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)
)

// OCIReference is a parsed oci:// source: oci://<host>/<repository> with
// either @sha256:<hex> naming the root image index or manifest, or :<tag>.
// Only plugin install accepts a tag, which it resolves to a digest.
type OCIReference struct {
	host, repository string
	// digest is the SHA256 hex of the root index or manifest; empty when
	// tag names the root instead.
	digest, tag string
}

// Host returns the registry host, with its port if the source names one.
func (r OCIReference) Host() string { return r.host }

// Repository returns the repository name within the registry.
func (r OCIReference) Repository() string { return r.repository }

// Digest returns the SHA256 hex of the root index or manifest, or "" for a
// tag reference.
func (r OCIReference) Digest() string { return r.digest }

// Tag returns the tag of a tag reference, or "" for a digest reference.
func (r OCIReference) Tag() string { return r.tag }

// WithDigest returns the digest reference to the same repository.
func (r OCIReference) WithDigest(digest string) (OCIReference, error) {
	if err := CheckDigest(digest); err != nil {
		return OCIReference{}, err
	}
	return OCIReference{host: r.host, repository: r.repository, digest: digest}, nil
}

// String returns the normalized source.
func (r OCIReference) String() string {
	if r.digest == "" {
		return "oci://" + r.host + "/" + r.repository + ":" + r.tag
	}
	return "oci://" + r.host + "/" + r.repository + "@sha256:" + r.digest
}

// IsOCI reports whether source uses the oci:// scheme.
func IsOCI(source string) bool {
	scheme, _, ok := strings.Cut(source, "://")
	return ok && strings.EqualFold(scheme, "oci")
}

// ParseOCI parses oci://<host>/<repository>@sha256:<hex> and refuses a tag.
func ParseOCI(source string) (OCIReference, error) { return parseOCI(source, false) }

// ParseOCITag parses oci://<host>/<repository>:<tag> or a digest reference.
// Only plugin install accepts a tag, which it resolves to a digest.
func ParseOCITag(source string) (OCIReference, error) { return parseOCI(source, true) }

func parseOCI(source string, allowTag bool) (OCIReference, error) {
	if !IsOCI(source) {
		return OCIReference{}, errors.New("not an oci:// reference")
	}
	rest := source[len("oci://"):]
	host, path, ok := strings.Cut(rest, "/")
	if !ok || !ociHostPattern.MatchString(host) || strings.ContainsAny(path, "?#") {
		// The source is not echoed: its host part may hold credentials.
		return OCIReference{}, errors.New("invalid OCI reference: use oci://<host>/<repository>@sha256:<64 hex digits>")
	}
	canonical, err := canonicalOCIHost(host)
	if err != nil {
		return OCIReference{}, errors.New("invalid OCI reference: use oci://<host>/<repository>@sha256:<64 hex digits>")
	}
	ref := OCIReference{host: canonical}
	if repository, digest, found := strings.Cut(path, "@"); found {
		hexDigest, ok := strings.CutPrefix(digest, "sha256:")
		if !ok || CheckDigest(hexDigest) != nil {
			return OCIReference{}, fmt.Errorf("invalid OCI reference %q: the digest must be sha256:<64 lowercase hex digits>", source)
		}
		ref.repository, ref.digest = repository, hexDigest
	} else {
		ref.repository = path
		if repository, tag, found := strings.CutLast(path, ":"); found {
			ref.repository, ref.tag = repository, tag
		}
		if !allowTag {
			return OCIReference{}, fmt.Errorf("OCI reference %q must pin a digest: oci://<host>/<repository>@sha256:<64 hex digits>; actionlint plugin install resolves a tag to its digest", source)
		}
		if ref.tag == "" {
			return OCIReference{}, fmt.Errorf("OCI reference %q names neither a tag nor a digest", source)
		}
	}
	if !ociRepositoryPattern.MatchString(ref.repository) {
		return OCIReference{}, fmt.Errorf("invalid OCI repository %q: use lowercase path components", ref.repository)
	}
	return ref, nil
}

func canonicalOCIHost(host string) (string, error) {
	u := url.URL{Host: host}
	name, err := endpoint.Host(u.Hostname())
	if err != nil {
		return "", err
	}
	authority, err := endpoint.Of(&url.URL{Scheme: ociScheme(name), Host: endpoint.HostPort(name, u.Port())})
	if err != nil {
		return "", err
	}
	return authority.URLHost(), nil
}

func ociScheme(host string) string {
	ip, err := netip.ParseAddr(host)
	if strings.EqualFold(host, "localhost") || err == nil && ip.IsLoopback() {
		return "http"
	}
	return "https"
}

// Endpoint returns the HTTP authority used by the OCI client. Loopback sources
// use plain HTTP like Docker; public-only policy still rejects their addresses.
func (r OCIReference) Endpoint() *url.URL {
	u := &url.URL{Host: r.host}
	u.Scheme = ociScheme(u.Hostname())
	return u
}

// NormalizeSource roots filesystem paths in directory while preserving URI
// identity, including queries and digest pins. local is empty for remote URLs;
// otherwise it identifies the local file consumed by the artifact resolver.
// It validates the location without reading files or making network requests.
func NormalizeSource(source, directory string) (reference, local string, err error) {
	if IsOCI(source) {
		ref, err := ParseOCI(source)
		if err != nil {
			return "", "", err
		}
		return ref.String(), "", nil
	}
	location, err := ParseModuleLocation(source)
	if err != nil {
		return "", "", err
	}
	if location.Endpoint != nil {
		return source, location.Local, nil
	}
	local = location.File(directory)
	return local, local, nil
}

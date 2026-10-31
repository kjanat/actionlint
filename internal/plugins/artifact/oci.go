package artifact

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

// OCI artifacts. An oci:// source names an image index or an image manifest
// by digest: oci://<host>/<repository>@sha256:<hex>. An index selects one
// image manifest per platform; a lone image manifest serves every platform.
// The selected image manifest has exactly one layer with a primary media
// type: a WebAssembly module or a tool provider manifest. Every other layer
// is a blob that primary layer names by digest, such as the provider's
// executable or a transform program. Resolving the source stores the
// manifests and the primary layer only; the other layers are stored when the
// provider is prepared to run, or by plugin install and plugin sync, so a
// provider can run offline afterwards.
//
// This package parses oci:// sources but does not pull them: the oci
// subpackage does, through oras-go, and callers pass its Pull as
// Options.PullOCI. That keeps oras-go, which links os/exec for credential
// helpers, out of the WebAssembly runtimes that import this package.
const (
	// MediaTypeWasm is the layer media type of a WebAssembly module.
	MediaTypeWasm = "application/wasm"
	// MediaTypeProviderManifest is the layer media type of a tool provider
	// manifest.
	MediaTypeProviderManifest = "application/vnd.actionlint.tool-provider.v1+json"
	// AnyPlatform is the platform key of a lone image manifest, which is not
	// selected from an index.
	AnyPlatform = "any"
)

var (
	ociHostPattern       = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)
	ociRepositoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:\.|_|__|-+)[a-z0-9]+)*)*$`)
)

// OCIReference is a parsed oci:// source.
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
	ref := OCIReference{host: strings.ToLower(host)}
	if repository, digest, found := strings.Cut(path, "@"); found {
		hex, ok := strings.CutPrefix(digest, "sha256:")
		if !ok || CheckDigest(hex) != nil {
			return OCIReference{}, fmt.Errorf("invalid OCI reference %q: the digest must be sha256:<64 lowercase hex digits>", source)
		}
		ref.repository, ref.digest = repository, hex
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

// PullOCI pulls an oci:// digest reference and returns its primary layer.
// The oci subpackage's Pull is the implementation.
type PullOCI func(ctx context.Context, ref OCIReference, options Options) (*Artifact, error)

// HostPlatform is the platform key of this host, "<GOOS>/<GOARCH>".
func HostPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// OCIPlatform lists what the host reads on one platform of an oci:// source.
type OCIPlatform struct {
	// Manifest is the SHA256 hex of the platform's image manifest.
	Manifest string `json:"manifest"`
	// Blobs are the SHA256 hex digests of the manifest's layers.
	Blobs []string `json:"blobs"`
}

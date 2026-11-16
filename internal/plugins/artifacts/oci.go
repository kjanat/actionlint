package artifacts

import (
	"context"
	"runtime"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
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
// Package pluginreference parses oci:// sources. This package does not pull
// them either: the oci subpackage does, through oras-go, and callers pass its
// Pull as Options.PullOCI. That keeps oras-go, which links os/exec for
// credential helpers, out of the WebAssembly runtimes that import this package.
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

// PullOCI pulls an oci:// digest reference and returns its primary layer.
// The oci subpackage's Pull is the implementation.
type PullOCI func(ctx context.Context, ref pluginreference.OCIReference, options Options) (*Artifact, error)

// HostPlatform is the platform key of this host, "<GOOS>/<GOARCH>".
func HostPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// OCIPlatform lists what the host reads on one platform of an oci:// source.
type OCIPlatform struct {
	// Manifest is the SHA256 hex of the platform's image manifest.
	Manifest string `json:"manifest"`
	// Blobs are the SHA256 hex digests of the manifest's layers.
	Blobs []string `json:"blobs"`
}

package artifacts

import (
	"context"
	"runtime"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

// The selected image manifest of an oci:// source has exactly one primary
// layer, a WebAssembly module or a tool provider manifest, and the primary
// layer names every other layer by digest.
//
// oras-go links os/exec for credential helpers and must stay out of this
// package, which WebAssembly runtimes import.
const (
	MediaTypeWasm             = "application/wasm"
	MediaTypeProviderManifest = "application/vnd.actionlint.tool-provider.v1+json"
	// AnyPlatform keys a lone image manifest.
	AnyPlatform = "any"
)

// PullOCI matches the oci subpackage's Pull.
type PullOCI func(ctx context.Context, ref pluginreference.OCIReference, options Options) (*Artifact, error)

func HostPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

type OCIPlatform struct {
	// Manifest is the SHA256 hex of the platform's image manifest.
	Manifest string `json:"manifest"`
	// Blobs are the SHA256 hex digests of the manifest's layers.
	Blobs []string `json:"blobs"`
}

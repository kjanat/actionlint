package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

// ArtifactOptions configures bounded acquisition and pinned download caching.
// PullOCI is explicitly supplied; this package does not choose an OCI client.
type ArtifactOptions struct {
	// CacheDir stores verified downloads pinned by SHA256. Empty disables caching.
	CacheDir string
	// Timeout bounds the whole acquisition; zero selects 30 seconds.
	Timeout time.Duration
	// MaxBytes bounds distributed and decompressed bytes; zero selects 128 MiB.
	MaxBytes int64
	// PullOCI acquires OCI digest references. Nil refuses OCI sources.
	PullOCI PullOCI
}

// ArtifactKind identifies the format of an acquired plugin.
type ArtifactKind string

const (
	// CoreArtifact is a core WebAssembly module.
	CoreArtifact ArtifactKind = "core"
	// ComponentArtifact is a WebAssembly component.
	ComponentArtifact ArtifactKind = "component"
	// ProviderManifestArtifact is a native tool provider manifest.
	ProviderManifestArtifact ArtifactKind = "manifest"
)

// ArtifactInfo describes distributed bytes and their untrusted static metadata.
// SHA256 covers distributed bytes, before optional gzip decompression.
type ArtifactInfo struct {
	Source     string          `json:"source"`
	Kind       ArtifactKind    `json:"kind"`
	SHA256     string          `json:"sha256"`
	Size       int             `json:"size"`
	ModuleSize int             `json:"module_size"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Platform   string          `json:"platform,omitempty"`
	Manifest   string          `json:"manifest,omitempty"`
	Blobs      []string        `json:"blobs,omitempty"`
}

// Artifact is an owned snapshot supplied by an OCI acquisition hook.
type Artifact struct {
	Info  ArtifactInfo
	Bytes []byte
}

// OCIReference is a validated OCI source. Its private representation keeps URI
// parsing in one authority without publishing an implementation package type.
type OCIReference struct{ reference pluginartifact.OCIReference }

// ParseOCI parses a digest-pinned OCI source and refuses tags.
func ParseOCI(source string) (OCIReference, error) {
	ref, err := pluginartifact.ParseOCI(source)
	return OCIReference{reference: ref}, err
}

// ParseOCITag parses a tagged or digest-pinned OCI source.
func ParseOCITag(source string) (OCIReference, error) {
	ref, err := pluginartifact.ParseOCITag(source)
	return OCIReference{reference: ref}, err
}

// Host returns the registry host and optional port.
func (r OCIReference) Host() string { return r.reference.Host() }

// Repository returns the repository within the registry.
func (r OCIReference) Repository() string { return r.reference.Repository() }

// Digest returns the root SHA256 hex, or an empty string for a tag.
func (r OCIReference) Digest() string { return r.reference.Digest() }

// Tag returns the tag, or an empty string for a digest reference.
func (r OCIReference) Tag() string { return r.reference.Tag() }

// String returns the normalized OCI source.
func (r OCIReference) String() string { return r.reference.String() }

// WithDigest returns a digest-pinned reference to the same repository.
func (r OCIReference) WithDigest(digest string) (OCIReference, error) {
	ref, err := r.reference.WithDigest(digest)
	return OCIReference{reference: ref}, err
}

// PullOCI acquires the selected primary layer. Options contains effective
// acquisition bounds and the original hook; adapters do not perform another pull.
type PullOCI func(context.Context, OCIReference, ArtifactOptions) (*Artifact, error)

func artifactOptionsToInternal(o ArtifactOptions) pluginartifact.Options {
	options := pluginartifact.Options{CacheDir: o.CacheDir, Timeout: o.Timeout, MaxBytes: o.MaxBytes}
	if o.PullOCI != nil {
		pull := o.PullOCI
		options.PullOCI = func(ctx context.Context, ref pluginartifact.OCIReference, effective pluginartifact.Options) (*pluginartifact.Artifact, error) {
			acquired, err := pull(ctx, OCIReference{reference: ref}, ArtifactOptions{CacheDir: effective.CacheDir, Timeout: effective.Timeout, MaxBytes: effective.MaxBytes, PullOCI: pull})
			if err != nil {
				return nil, err
			}
			if acquired == nil {
				return nil, errors.New("OCI acquisition hook returned no artifact")
			}
			return &pluginartifact.Artifact{Info: artifactInfoToInternal(acquired.Info), Bytes: bytes.Clone(acquired.Bytes)}, nil
		}
	}
	return options
}

func artifactInfoFromInternal(i pluginartifact.Info) ArtifactInfo {
	return ArtifactInfo{Source: i.Source, Kind: ArtifactKind(i.Kind), SHA256: i.SHA256, Size: i.Size, ModuleSize: i.ModuleSize, Metadata: slices.Clone(i.Metadata), Platform: i.Platform, Manifest: i.Manifest, Blobs: slices.Clone(i.Blobs)}
}

func artifactInfoToInternal(i ArtifactInfo) pluginartifact.Info {
	return pluginartifact.Info{Source: i.Source, Kind: pluginartifact.Kind(i.Kind), SHA256: i.SHA256, Size: i.Size, ModuleSize: i.ModuleSize, Metadata: slices.Clone(i.Metadata), Platform: i.Platform, Manifest: i.Manifest, Blobs: slices.Clone(i.Blobs)}
}

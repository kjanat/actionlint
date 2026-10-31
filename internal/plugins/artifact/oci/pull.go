// Package oci pulls oci:// plugin sources through oras-go. It is the only
// package that imports oras-go: oras-go is the transport, and every manifest
// and blob it fetches is verified against its digest and published to the
// artifact Store, which stays the cache. Pass Pull as
// artifact.Options.PullOCI.
package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/oras-project/oras-go/v3/registry/remote/retry"
)

const (
	maxManifestBytes   = 4 << 20
	dockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	dockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
)

// loopbackHost reports whether host, with an optional port, is localhost or
// a loopback address. Like docker, the host reaches such registries over
// plain HTTP.
func loopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	if strings.EqualFold(name, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(name)
	return err == nil && addr.IsLoopback()
}

// repository returns an anonymous oras-go client for the repository of ref.
// The client answers the registry's bearer challenge with an anonymous
// token and retries as oras-go's retry client does, honouring Retry-After;
// the caller's context deadline bounds every pull.
func repository(ref artifact.OCIReference) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref.Host() + "/" + ref.Repository())
	if err != nil {
		return nil, err
	}
	client := &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{"User-Agent": {"actionlint"}},
		Cache:  auth.NewCache(),
	}
	if loopbackHost(ref.Host()) {
		// A loopback registry may name a loopback token realm; every
		// other default block still applies.
		client.BlockedRealmPrefixes = slices.DeleteFunc(auth.DefaultBlockedRealmPrefixes(), func(prefix netip.Prefix) bool { return prefix.Addr().IsLoopback() })
		repo.Registry.PlainHTTP = true
	}
	repo.Registry.Client = client
	return repo, nil
}

// document holds the fields of an image index or image manifest that the
// host reads.
type document struct {
	MediaType string               `json:"mediaType"`
	Manifests []ocispec.Descriptor `json:"manifests"`
	Layers    []ocispec.Descriptor `json:"layers"`
}

func (d document) index() bool {
	return d.MediaType == ocispec.MediaTypeImageIndex || d.MediaType == dockerManifestList || d.MediaType == "" && d.Manifests != nil
}

// puller fetches the manifests and blobs of one oci:// source through the
// store.
type puller struct {
	ref   artifact.OCIReference
	repo  *remote.Repository
	store artifact.Store
	limit int64
}

func newPuller(ref artifact.OCIReference, options artifact.Options) (*puller, error) {
	repo, err := repository(ref)
	if err != nil {
		return nil, err
	}
	limit := options.MaxBytes
	if limit <= 0 {
		limit = artifact.DefaultMaxBytes
	}
	return &puller{ref: ref, repo: repo, store: artifact.Store{Dir: options.CacheDir}, limit: limit}, nil
}

// withTimeout bounds ctx by options.Timeout, or artifact.DefaultTimeout.
func withTimeout(ctx context.Context, options artifact.Options) (context.Context, context.CancelFunc) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = artifact.DefaultTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

// cached returns the stored blob digest. A missing or corrupt blob is not an
// error: the caller fetches it again.
func (p *puller) cached(ctx context.Context, digest string, limit int64) ([]byte, bool, error) {
	data, err := p.store.Read(ctx, digest, limit)
	switch {
	case err == nil:
		return data, true, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, artifact.ErrCorrupt):
		return nil, false, nil
	default:
		return nil, false, err
	}
}

// manifest returns the verified index or image manifest with SHA256 digest.
func (p *puller) manifest(ctx context.Context, digest string) (document, error) {
	data, ok, err := p.cached(ctx, digest, maxManifestBytes)
	if err != nil {
		return document{}, err
	}
	if !ok {
		desc, body, err := p.repo.FetchReference(ctx, "sha256:"+digest)
		if err != nil {
			return document{}, err
		}
		defer body.Close()
		if desc.Digest.String() != "sha256:"+digest || desc.Size > maxManifestBytes {
			return document{}, fmt.Errorf("manifest sha256:%s: registry reported %s with %d bytes", digest, desc.Digest, desc.Size)
		}
		if data, err = content.ReadAll(body, desc); err != nil {
			return document{}, fmt.Errorf("manifest sha256:%s: %w", digest, err)
		}
		if err := p.store.Publish(ctx, digest, data); err != nil {
			return document{}, err
		}
	}
	var parsed document
	if err := json.Unmarshal(data, &parsed); err != nil {
		return document{}, fmt.Errorf("manifest sha256:%s: %w", digest, err)
	}
	return parsed, nil
}

// blob returns the verified bytes of the layer desc.
func (p *puller) blob(ctx context.Context, desc ocispec.Descriptor) ([]byte, error) {
	digest, err := descriptorDigest(desc)
	if err != nil {
		return nil, err
	}
	if desc.Size < 0 || desc.Size > p.limit {
		return nil, fmt.Errorf("layer sha256:%s: %d bytes exceeds %d byte limit", digest, desc.Size, p.limit)
	}
	data, ok, err := p.cached(ctx, digest, p.limit)
	if err != nil || ok {
		return data, err
	}
	data, err = content.FetchAll(ctx, p.repo.Blobs(), desc)
	if err != nil {
		return nil, fmt.Errorf("layer sha256:%s: %w", digest, err)
	}
	if err := p.store.Publish(ctx, digest, data); err != nil {
		return nil, err
	}
	return data, nil
}

// platforms returns the image manifest digest of every platform the root
// serves, keyed by "<os>/<arch>", or artifact.AnyPlatform for a lone image
// manifest.
func (p *puller) platforms(ctx context.Context) (map[string]string, error) {
	root, err := p.manifest(ctx, p.ref.Digest())
	if err != nil {
		return nil, err
	}
	if !root.index() {
		if root.MediaType != "" && root.MediaType != ocispec.MediaTypeImageManifest && root.MediaType != dockerManifest {
			return nil, fmt.Errorf("unsupported OCI media type %q", root.MediaType)
		}
		return map[string]string{artifact.AnyPlatform: p.ref.Digest()}, nil
	}
	platforms := map[string]string{}
	for _, entry := range root.Manifests {
		if entry.Platform == nil || entry.Platform.OS == "" || entry.Platform.Architecture == "" {
			continue
		}
		digest, err := descriptorDigest(entry)
		if err != nil {
			return nil, err
		}
		key := entry.Platform.OS + "/" + entry.Platform.Architecture
		if _, ok := platforms[key]; !ok {
			platforms[key] = digest
		}
	}
	return platforms, nil
}

// layers returns the layers of the image manifest digest and the index of
// its primary layer.
func (p *puller) layers(ctx context.Context, digest string) ([]ocispec.Descriptor, int, error) {
	parsed, err := p.manifest(ctx, digest)
	if err != nil {
		return nil, 0, err
	}
	if parsed.index() {
		return nil, 0, fmt.Errorf("manifest sha256:%s: an index cannot be a platform's image manifest", digest)
	}
	primary := -1
	for i, layer := range parsed.Layers {
		if layer.MediaType != artifact.MediaTypeWasm && layer.MediaType != artifact.MediaTypeProviderManifest {
			continue
		}
		if primary >= 0 {
			return nil, 0, fmt.Errorf("manifest sha256:%s has more than one %s or %s layer", digest, artifact.MediaTypeWasm, artifact.MediaTypeProviderManifest)
		}
		primary = i
	}
	if primary < 0 {
		return nil, 0, fmt.Errorf("manifest sha256:%s has no %s or %s layer", digest, artifact.MediaTypeWasm, artifact.MediaTypeProviderManifest)
	}
	return parsed.Layers, primary, nil
}

// Pull pulls the image manifest of ref for this host and every layer it
// lists into the store, and returns its primary layer: a WebAssembly module,
// whose kind Sniff detects, or a tool provider manifest, which is also the
// artifact's metadata. It implements artifact.PullOCI.
func Pull(ctx context.Context, ref artifact.OCIReference, options artifact.Options) (*artifact.Artifact, error) {
	ctx, cancel := withTimeout(ctx, options)
	defer cancel()
	pull, err := newPuller(ref, options)
	if err != nil {
		return nil, err
	}
	platforms, err := pull.platforms(ctx)
	if err != nil {
		return nil, err
	}
	platform := artifact.HostPlatform()
	manifest, ok := platforms[platform]
	if !ok {
		platform = artifact.AnyPlatform
		if manifest, ok = platforms[artifact.AnyPlatform]; !ok {
			return nil, fmt.Errorf("no image manifest for platform %s; the index serves %s", artifact.HostPlatform(), strings.Join(slices.Sorted(maps.Keys(platforms)), ", "))
		}
	}
	layers, primary, err := pull.layers(ctx, manifest)
	if err != nil {
		return nil, err
	}
	info := artifact.Info{Source: ref.String(), SHA256: ref.Digest(), Manifest: manifest, Platform: platform}
	var data []byte
	for i, layer := range layers {
		blob, err := pull.blob(ctx, layer)
		if err != nil {
			return nil, err
		}
		digest, err := descriptorDigest(layer)
		if err != nil {
			return nil, err
		}
		info.Blobs = append(info.Blobs, digest)
		if i == primary {
			data = blob
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info.Size, info.ModuleSize = len(data), len(data)
	if layers[primary].MediaType == artifact.MediaTypeProviderManifest {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			return nil, fmt.Errorf("tool provider manifest layer: %w", err)
		}
		if object == nil {
			return nil, errors.New("tool provider manifest layer must be a JSON object")
		}
		info.Kind, info.Metadata = artifact.ProviderManifest, data
	} else if info.Kind, info.Metadata, err = artifact.Sniff(data); err != nil {
		return nil, err
	}
	return &artifact.Artifact{Info: info, Bytes: data}, nil
}

// Describe returns the image manifest and layer digests of every platform
// the oci:// source serves, keyed like artifact.Info.Platform. It fetches
// manifests only, never layers, so it reads no executable of another
// platform.
func Describe(ctx context.Context, source string, options artifact.Options) (map[string]artifact.OCIPlatform, error) {
	ctx, cancel := withTimeout(ctx, options)
	defer cancel()
	ref, err := artifact.ParseOCI(source)
	if err != nil {
		return nil, err
	}
	pull, err := newPuller(ref, options)
	if err != nil {
		return nil, err
	}
	platforms, err := pull.platforms(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin artifact %s: %w", ref, err)
	}
	described := make(map[string]artifact.OCIPlatform, len(platforms))
	for platform, manifest := range platforms {
		layers, _, err := pull.layers(ctx, manifest)
		if err != nil {
			return nil, fmt.Errorf("plugin artifact %s: %w", ref, err)
		}
		entry := artifact.OCIPlatform{Manifest: manifest}
		for _, layer := range layers {
			digest, err := descriptorDigest(layer)
			if err != nil {
				return nil, err
			}
			entry.Blobs = append(entry.Blobs, digest)
		}
		described[platform] = entry
	}
	return described, nil
}

// Pin resolves an oci://<host>/<repository>:<tag> source to the digest the
// tag names now and returns the digest form. A digest source is returned
// normalized.
func Pin(ctx context.Context, source string, options artifact.Options) (string, error) {
	ctx, cancel := withTimeout(ctx, options)
	defer cancel()
	ref, err := artifact.ParseOCITag(source)
	if err != nil {
		return "", err
	}
	if ref.Digest() != "" {
		return ref.String(), nil
	}
	repo, err := repository(ref)
	if err != nil {
		return "", err
	}
	desc, err := repo.Resolve(ctx, ref.Tag())
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", ref, err)
	}
	digest, err := descriptorDigest(desc)
	if err != nil {
		return "", err
	}
	pinned, err := ref.WithDigest(digest)
	if err != nil {
		return "", err
	}
	return pinned.String(), nil
}

// descriptorDigest returns the SHA256 hex of desc and refuses other
// algorithms.
func descriptorDigest(desc ocispec.Descriptor) (string, error) {
	hex, ok := strings.CutPrefix(desc.Digest.String(), "sha256:")
	if !ok || artifact.CheckDigest(hex) != nil {
		return "", fmt.Errorf("unsupported OCI digest %q: want sha256:<64 lowercase hex digits>", desc.Digest)
	}
	return hex, nil
}

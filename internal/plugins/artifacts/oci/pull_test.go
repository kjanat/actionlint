package oci_test

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/ocitest"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// providerPackage publishes a provider manifest and one executable per
// platform as an index, the shape of a multi-platform binary provider.
type providerPackage struct {
	index, manifest, executable ocitest.Descriptor
	reference                   string
}

func publishProvider(t *testing.T, registry *ocitest.Registry) providerPackage {
	t.Helper()
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, []byte(`{"schema_version":1,"transport":"process"}`))
	platforms := map[string]ocitest.Descriptor{}
	var host ocitest.Descriptor
	for _, platform := range []string{"linux/amd64", "darwin/arm64", "windows/amd64", artifacts.HostPlatform()} {
		executable := registry.Blob("application/octet-stream", []byte("executable for "+platform))
		platforms[platform] = registry.Manifest(t, manifest, executable)
		if platform == artifacts.HostPlatform() {
			host = executable
		}
	}
	index := registry.Index(t, platforms)
	return providerPackage{index: index, manifest: manifest, executable: host, reference: registry.Reference(index)}
}

// options pulls oci:// sources into cache, or keeps nothing when cache is "".
func options(t *testing.T, source, cache string) artifacts.Options {
	t.Helper()
	grant, err := artifacts.ExplicitSource(source)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts.Options{CacheDir: cache, PullOCI: oci.Pull, Network: grant}
}

func TestPullSelectsHostPlatform(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	pulled, err := artifacts.Resolve(t.Context(), pkg.reference, options(t, pkg.reference, cache))
	if err != nil {
		t.Fatal(err)
	}
	info := pulled.Info
	if info.Kind != artifacts.ProviderManifest || info.SHA256 != pkg.index.Hex() || info.Platform != artifacts.HostPlatform() || info.Source != pkg.reference {
		t.Fatalf("info = %+v", info)
	}
	if !bytes.Equal(pulled.Bytes, []byte(`{"schema_version":1,"transport":"process"}`)) || !bytes.Equal(info.Metadata, pulled.Bytes) {
		t.Fatalf("primary layer = %q, metadata = %q", pulled.Bytes, info.Metadata)
	}
	if !slices.Equal(info.Blobs, []string{pkg.manifest.Hex(), pkg.executable.Hex()}) {
		t.Fatalf("blobs = %q", info.Blobs)
	}
	store := artifacts.Store{Dir: cache}
	for _, digest := range []string{info.SHA256, info.Manifest, pkg.manifest.Hex()} {
		if _, err := store.Read(t.Context(), digest, 1<<20); err != nil {
			t.Fatalf("store lacks %s: %v", digest, err)
		}
	}
	// Resolving reads the provider manifest only; the executable waits
	// until the provider runs.
	if got := registry.Fetches(pkg.executable); got != 0 {
		t.Fatalf("Resolve fetched the executable %d times", got)
	}
	executable, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(t, pkg.reference, cache))
	if err != nil || string(executable) != "executable for "+artifacts.HostPlatform() {
		t.Fatalf("Layer = %q, %v", executable, err)
	}
	registry.Close()
	if _, err := artifacts.Resolve(t.Context(), pkg.reference, options(t, pkg.reference, cache)); err != nil {
		t.Fatalf("stored package not usable offline: %v", err)
	}
	if _, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(t, pkg.reference, cache)); err != nil {
		t.Fatalf("stored executable not usable offline: %v", err)
	}
}

func TestPullLayersStoresEveryHostLayer(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	if err := oci.PullLayers(t.Context(), pkg.reference, options(t, pkg.reference, cache)); err != nil {
		t.Fatal(err)
	}
	for _, layer := range []ocitest.Descriptor{pkg.manifest, pkg.executable} {
		if _, err := (artifacts.Store{Dir: cache}).Read(t.Context(), layer.Hex(), 1<<20); err != nil {
			t.Fatalf("store lacks %s: %v", layer.Hex(), err)
		}
	}
}

func TestLayerRefusesDigestOutsideManifest(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	other := registry.Blob("application/octet-stream", []byte("not a layer"))
	if _, err := oci.Layer(t.Context(), pkg.reference, other.Hex(), options(t, pkg.reference, "")); err == nil || !strings.Contains(err.Error(), "is not a layer") {
		t.Fatalf("error = %v", err)
	}
	if registry.Fetches(other) != 0 {
		t.Fatal("Layer fetched a blob its image manifest does not list")
	}
}

func TestPullWasmLayer(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	wasm := registry.Blob(artifacts.MediaTypeWasm, []byte("\x00asm\x01\x00\x00\x00"))
	root := registry.Manifest(t, wasm)
	pulled, err := artifacts.Resolve(t.Context(), registry.Reference(root), options(t, registry.Reference(root), ""))
	if err != nil {
		t.Fatal(err)
	}
	if pulled.Info.Kind != artifacts.Core || pulled.Info.Platform != artifacts.AnyPlatform || pulled.Info.Manifest != root.Hex() {
		t.Fatalf("info = %+v", pulled.Info)
	}
}

func TestPullRejectsBlobNotMatchingDigest(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	registry.Tamper(pkg.executable, bytes.Repeat([]byte("x"), int(pkg.executable.Size)))
	cache := t.TempDir()
	_, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(t, pkg.reference, cache))
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("forged blob accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, pkg.executable.Hex())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forged blob published to the store: %v", err)
	}
}

func TestPullRefetchesHalfWrittenBlob(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	if err := oci.PullLayers(t.Context(), pkg.reference, options(t, pkg.reference, cache)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache, pkg.executable.Hex())
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// An interrupted download that left the first half of the blob.
	if err := os.WriteFile(path, original[:len(original)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (artifacts.Store{Dir: cache}).Read(t.Context(), pkg.executable.Hex(), 1<<20); !errors.Is(err, artifacts.ErrCorrupt) {
		t.Fatalf("half-written blob read as %v", err)
	}
	if _, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(t, pkg.reference, cache)); err != nil {
		t.Fatal(err)
	}
	if got := registry.Fetches(pkg.executable); got != 2 {
		t.Fatalf("executable fetched %d times, want 2", got)
	}
	if repaired, err := os.ReadFile(path); err != nil || !bytes.Equal(repaired, original) {
		t.Fatalf("store not repaired: %q, %v", repaired, err)
	}
}

func TestResolveWithoutPullerRefusesOCI(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	if _, err := artifacts.Resolve(t.Context(), pkg.reference, artifacts.Options{}); err == nil || !strings.Contains(err.Error(), "cannot pull oci://") {
		t.Fatalf("error = %v", err)
	}
}

func TestPinResolvesTag(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	registry.Tag("v1", pkg.index)
	tagged := "oci://" + registry.Host() + "/" + ocitest.Repository + ":v1"
	pinned, err := oci.Pin(t.Context(), tagged, options(t, tagged, ""))
	if err != nil || pinned != pkg.reference {
		t.Fatalf("Pin = %q, %v; want %q", pinned, err, pkg.reference)
	}
	if again, err := oci.Pin(t.Context(), pinned, options(t, pinned, "")); err != nil || again != pinned {
		t.Fatalf("digest reference changed: %q, %v", again, err)
	}
}

func TestDescribeReadsNoLayers(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	described, err := oci.Describe(t.Context(), pkg.reference, options(t, pkg.reference, ""))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"darwin/arm64", "linux/amd64", "windows/amd64"}
	if !slices.Contains(want, artifacts.HostPlatform()) {
		want = append(want, artifacts.HostPlatform())
	}
	if got := slices.Sorted(maps.Keys(described)); !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Fatalf("platforms = %q, want %q", got, want)
	}
	host := described[artifacts.HostPlatform()]
	if host.Manifest == "" || !slices.Equal(host.Blobs, []string{pkg.manifest.Hex(), pkg.executable.Hex()}) {
		t.Fatalf("host platform = %+v", host)
	}
	if registry.Fetches(pkg.executable) != 0 || registry.Fetches(pkg.manifest) != 0 {
		t.Fatal("Describe fetched a layer")
	}
}

func TestDigestPlanReadsOnlyHostManifests(t *testing.T) {
	t.Parallel()
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	digests, err := oci.Digests(t.Context(), pkg.reference, options(t, pkg.reference, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) != 4 || !slices.Contains(digests, pkg.executable.Hex()) || !slices.Contains(digests, pkg.manifest.Hex()) {
		t.Fatalf("incomplete OCI cache plan: %v", digests)
	}
	if registry.Fetches(pkg.executable) != 0 || registry.Fetches(pkg.manifest) != 0 {
		t.Fatal("cache planning fetched an artifact layer")
	}
}

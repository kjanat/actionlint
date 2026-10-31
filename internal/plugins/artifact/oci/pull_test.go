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

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact/oci"
	"actionlint.kjanat.dev/v2/internal/testutil/ocitest"
)

// providerPackage publishes a provider manifest and one executable per
// platform as an index, the shape of a multi-platform binary provider.
type providerPackage struct {
	index, manifest, executable ocitest.Descriptor
	reference                   string
}

func publishProvider(t *testing.T, registry *ocitest.Registry) providerPackage {
	t.Helper()
	manifest := registry.Blob(artifact.MediaTypeProviderManifest, []byte(`{"schema_version":1}`))
	platforms := map[string]ocitest.Descriptor{}
	var host ocitest.Descriptor
	for _, platform := range []string{"linux/amd64", "darwin/arm64", "windows/amd64", artifact.HostPlatform()} {
		executable := registry.Blob("application/octet-stream", []byte("executable for "+platform))
		platforms[platform] = registry.Manifest(t, manifest, executable)
		if platform == artifact.HostPlatform() {
			host = executable
		}
	}
	index := registry.Index(t, platforms)
	return providerPackage{index: index, manifest: manifest, executable: host, reference: registry.Reference(index)}
}

// options pulls oci:// sources into cache, or keeps nothing when cache is "".
func options(cache string) artifact.Options {
	return artifact.Options{CacheDir: cache, PullOCI: oci.Pull}
}

func TestPullSelectsHostPlatform(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	pulled, err := artifact.Resolve(t.Context(), pkg.reference, options(cache))
	if err != nil {
		t.Fatal(err)
	}
	info := pulled.Info
	if info.Kind != artifact.ProviderManifest || info.SHA256 != pkg.index.Hex() || info.Platform != artifact.HostPlatform() || info.Source != pkg.reference {
		t.Fatalf("info = %+v", info)
	}
	if !bytes.Equal(pulled.Bytes, []byte(`{"schema_version":1}`)) || !bytes.Equal(info.Metadata, pulled.Bytes) {
		t.Fatalf("primary layer = %q, metadata = %q", pulled.Bytes, info.Metadata)
	}
	if !slices.Equal(info.Blobs, []string{pkg.manifest.Hex(), pkg.executable.Hex()}) {
		t.Fatalf("blobs = %q", info.Blobs)
	}
	store := artifact.Store{Dir: cache}
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
	executable, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(cache))
	if err != nil || string(executable) != "executable for "+artifact.HostPlatform() {
		t.Fatalf("Layer = %q, %v", executable, err)
	}
	registry.Close()
	if _, err := artifact.Resolve(t.Context(), pkg.reference, options(cache)); err != nil {
		t.Fatalf("stored package not usable offline: %v", err)
	}
	if _, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(cache)); err != nil {
		t.Fatalf("stored executable not usable offline: %v", err)
	}
}

func TestPullLayersStoresEveryHostLayer(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	if err := oci.PullLayers(t.Context(), pkg.reference, options(cache)); err != nil {
		t.Fatal(err)
	}
	for _, layer := range []ocitest.Descriptor{pkg.manifest, pkg.executable} {
		if _, err := (artifact.Store{Dir: cache}).Read(t.Context(), layer.Hex(), 1<<20); err != nil {
			t.Fatalf("store lacks %s: %v", layer.Hex(), err)
		}
	}
}

func TestLayerRefusesDigestOutsideManifest(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	other := registry.Blob("application/octet-stream", []byte("not a layer"))
	if _, err := oci.Layer(t.Context(), pkg.reference, other.Hex(), options("")); err == nil || !strings.Contains(err.Error(), "is not a layer") {
		t.Fatalf("error = %v", err)
	}
	if registry.Fetches(other) != 0 {
		t.Fatal("Layer fetched a blob its image manifest does not list")
	}
}

func TestPullWasmLayer(t *testing.T) {
	registry := ocitest.New(t)
	wasm := registry.Blob(artifact.MediaTypeWasm, []byte("\x00asm\x01\x00\x00\x00"))
	root := registry.Manifest(t, wasm)
	pulled, err := artifact.Resolve(t.Context(), registry.Reference(root), options(""))
	if err != nil {
		t.Fatal(err)
	}
	if pulled.Info.Kind != artifact.Core || pulled.Info.Platform != artifact.AnyPlatform || pulled.Info.Manifest != root.Hex() {
		t.Fatalf("info = %+v", pulled.Info)
	}
}

func TestPullRejectsBlobNotMatchingDigest(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	registry.Tamper(pkg.executable, bytes.Repeat([]byte("x"), int(pkg.executable.Size)))
	cache := t.TempDir()
	_, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(cache))
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("forged blob accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, pkg.executable.Hex())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forged blob published to the store: %v", err)
	}
}

func TestPullRefetchesHalfWrittenBlob(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	cache := t.TempDir()
	if err := oci.PullLayers(t.Context(), pkg.reference, options(cache)); err != nil {
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
	if _, err := (artifact.Store{Dir: cache}).Read(t.Context(), pkg.executable.Hex(), 1<<20); !errors.Is(err, artifact.ErrCorrupt) {
		t.Fatalf("half-written blob read as %v", err)
	}
	if _, err := oci.Layer(t.Context(), pkg.reference, pkg.executable.Hex(), options(cache)); err != nil {
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
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	if _, err := artifact.Resolve(t.Context(), pkg.reference, artifact.Options{}); err == nil || !strings.Contains(err.Error(), "cannot pull oci://") {
		t.Fatalf("error = %v", err)
	}
}

func TestPinResolvesTag(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	registry.Tag("v1", pkg.index)
	pinned, err := oci.Pin(t.Context(), "oci://"+registry.Host()+"/"+ocitest.Repository+":v1", options(""))
	if err != nil || pinned != pkg.reference {
		t.Fatalf("Pin = %q, %v; want %q", pinned, err, pkg.reference)
	}
	if again, err := oci.Pin(t.Context(), pinned, options("")); err != nil || again != pinned {
		t.Fatalf("digest reference changed: %q, %v", again, err)
	}
}

func TestDescribeReadsNoLayers(t *testing.T) {
	registry := ocitest.New(t)
	pkg := publishProvider(t, registry)
	described, err := oci.Describe(t.Context(), pkg.reference, options(""))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"darwin/arm64", "linux/amd64", "windows/amd64"}
	if !slices.Contains(want, artifact.HostPlatform()) {
		want = append(want, artifact.HostPlatform())
	}
	if got := slices.Sorted(maps.Keys(described)); !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Fatalf("platforms = %q, want %q", got, want)
	}
	host := described[artifact.HostPlatform()]
	if host.Manifest == "" || !slices.Equal(host.Blobs, []string{pkg.manifest.Hex(), pkg.executable.Hex()}) {
		t.Fatalf("host platform = %+v", host)
	}
	if registry.Fetches(pkg.executable) != 0 || registry.Fetches(pkg.manifest) != 0 {
		t.Fatal("Describe fetched a layer")
	}
}

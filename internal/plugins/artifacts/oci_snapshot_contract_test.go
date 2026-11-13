package artifacts_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestOCIResolutionHookSnapshots(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	source := "oci://registry.example/tools/echo@sha256:" + digest
	cache := t.TempDir()
	metadata := json.RawMessage(`{"id":"echo"}`)
	module := []byte("module")
	blobs := []string{strings.Repeat("b", 64)}
	calls := 0
	options := pluginartifact.Options{CacheDir: cache, Timeout: time.Second, MaxBytes: 4096}
	options.PullOCI = func(ctx context.Context, ref pluginartifact.OCIReference, bounds pluginartifact.Options) (*pluginartifact.Artifact, error) {
		calls++
		if ref.String() != source || ref.Host() != "registry.example" || ref.Repository() != "tools/echo" || ref.Digest() != digest || ref.Tag() != "" {
			t.Fatalf("wrong reference: %s", ref)
		}
		if bounds.CacheDir != cache || bounds.Timeout != time.Second || bounds.MaxBytes != 4096 || bounds.PullOCI == nil {
			t.Fatalf("lost acquisition options: %+v", bounds)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("hook has no acquisition deadline")
		}
		return &pluginartifact.Artifact{Info: pluginartifact.Info{Source: source, Kind: pluginartifact.Core, SHA256: digest, Size: 12, ModuleSize: 10, Metadata: metadata, Platform: "any", Manifest: digest, Blobs: blobs}, Bytes: module}, nil
	}
	resolved, err := pluginartifact.Resolve(context.Background(), source, options)
	if err != nil {
		t.Fatal(err)
	}
	info := resolved.Info
	if calls != 1 || info.Source != source || info.Kind != pluginartifact.Core || info.SHA256 != digest || info.Size != 12 || info.ModuleSize != 10 || info.Platform != "any" || info.Manifest != digest || string(info.Metadata) != string(metadata) || len(info.Blobs) != 1 || info.Blobs[0] != blobs[0] {
		t.Fatalf("hook result not preserved: calls=%d info=%+v", calls, info)
	}
	metadata[0] = 'x'
	module[0] = 'x'
	blobs[0] = "changed"
	if string(info.Metadata) != `{"id":"echo"}` || info.Blobs[0] != strings.Repeat("b", 64) || string(resolved.Bytes) != "module" {
		t.Fatal("inspection retained mutable hook storage")
	}
}

func TestOCIResolutionHookDefaultsAndFailures(t *testing.T) {
	t.Parallel()
	source := "oci://registry.example/echo@sha256:" + strings.Repeat("a", 64)
	want := errors.New("hook refused")
	_, err := pluginartifact.Resolve(context.Background(), source, pluginartifact.Options{PullOCI: func(ctx context.Context, ref pluginartifact.OCIReference, options pluginartifact.Options) (*pluginartifact.Artifact, error) {
		if options.Timeout != 30*time.Second || options.MaxBytes != 128<<20 {
			t.Fatalf("missing default acquisition bounds: %+v", options)
		}
		return nil, want
	}})
	if !errors.Is(err, want) {
		t.Fatalf("hook error identity lost: %v", err)
	}
	_, err = pluginartifact.Resolve(context.Background(), source, pluginartifact.Options{PullOCI: func(context.Context, pluginartifact.OCIReference, pluginartifact.Options) (*pluginartifact.Artifact, error) {
		return nil, nil
	}})
	if err == nil || !strings.Contains(err.Error(), "no artifact") {
		t.Fatalf("nil hook result should fail: %v", err)
	}
}

func TestOCIReferenceIdentity(t *testing.T) {
	t.Parallel()
	ref, err := pluginartifact.ParseOCITag("oci://REGISTRY.example/tools/echo:v1")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Host() != "registry.example" || ref.Tag() != "v1" || ref.Digest() != "" {
		t.Fatalf("tag reference changed: %s", ref)
	}
	pinned, err := ref.WithDigest(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pluginartifact.ParseOCI(pinned.String())
	if err != nil || parsed != pinned || parsed.Tag() != "" || ref.Tag() != "v1" {
		t.Fatalf("digest conversion mutated original or lost identity: %s %s %v", ref, parsed, err)
	}
	if _, err := pluginartifact.ParseOCI(ref.String()); err == nil {
		t.Fatal("execution reference accepted an unpinned tag")
	}
}

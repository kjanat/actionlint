package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/plugin"
	"actionlint.kjanat.dev/v2/runner"
)

var (
	_ analyzer.Checker                = (*plugin.Component)(nil)
	_ runner.Provider                 = (*plugin.Component)(nil)
	_ analyzer.DescribedScriptBackend = (*plugin.Component)(nil)
	_ analyzer.ScriptConfigValidator  = (*plugin.Component)(nil)
	_ analyzer.ToolIntegrator         = (*plugin.Component)(nil)
	_ analyzer.DescribedScriptBackend = (*plugin.Engine)(nil)
	_ analyzer.ScriptConfigValidator  = (*plugin.Engine)(nil)
	_ *plugin.Component               = (*plugin.Analyzer)(nil)
)

func TestFacadeZeroValueGetters(t *testing.T) {
	var component plugin.Component
	if component.HasAnalyzer() || component.HasRunnerProvider() || component.HasScriptEngine() || component.Path() != "" || component.ArtifactDigest() != "" || component.Limits() != (plugin.Limits{}) {
		t.Fatal("zero component advertises loaded state")
	}
	if component.Descriptor().Name != "" || component.RunnerDescriptor().Name != "" || component.ScriptDescriptor().ID != "" || component.ToolIntegration() != nil {
		t.Fatal("zero component advertises a role descriptor")
	}
	var engine plugin.Engine
	if engine.Path() != "" || engine.CheckVersion() != 0 || engine.Limits() != (plugin.EngineLimits{}) || engine.Metadata().ID != "" || engine.ScriptDescriptor().ID != "" {
		t.Fatal("zero engine advertises loaded state")
	}
}

func TestFacadeOCIHook(t *testing.T) {
	digest := strings.Repeat("a", 64)
	source := "oci://registry.example/tools/echo@sha256:" + digest
	cache := t.TempDir()
	metadata := json.RawMessage(`{"id":"echo"}`)
	blobs := []string{strings.Repeat("b", 64)}
	calls := 0
	options := plugin.ArtifactOptions{CacheDir: cache, Timeout: time.Second, MaxBytes: 4096}
	options.PullOCI = func(ctx context.Context, ref plugin.OCIReference, bounds plugin.ArtifactOptions) (*plugin.Artifact, error) {
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
		return &plugin.Artifact{Info: plugin.ArtifactInfo{Source: source, Kind: plugin.CoreArtifact, SHA256: digest, Size: 12, ModuleSize: 10, Metadata: metadata, Platform: "any", Manifest: digest, Blobs: blobs}, Bytes: []byte("module")}, nil
	}
	info, err := plugin.Inspect(context.Background(), source, options)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || info.Source != source || info.Kind != plugin.CoreArtifact || info.SHA256 != digest || info.Size != 12 || info.ModuleSize != 10 || info.Platform != "any" || info.Manifest != digest || string(info.Metadata) != string(metadata) || len(info.Blobs) != 1 || info.Blobs[0] != blobs[0] {
		t.Fatalf("hook result not preserved: calls=%d info=%+v", calls, info)
	}
	metadata[0] = 'x'
	blobs[0] = "changed"
	if string(info.Metadata) != `{"id":"echo"}` || info.Blobs[0] != strings.Repeat("b", 64) {
		t.Fatal("inspection retained mutable hook storage")
	}
}

func TestFacadeOCIHookDefaultsAndFailures(t *testing.T) {
	source := "oci://registry.example/echo@sha256:" + strings.Repeat("a", 64)
	want := errors.New("hook refused")
	_, err := plugin.Inspect(context.Background(), source, plugin.ArtifactOptions{PullOCI: func(ctx context.Context, ref plugin.OCIReference, options plugin.ArtifactOptions) (*plugin.Artifact, error) {
		if options.Timeout != 30*time.Second || options.MaxBytes != 128<<20 {
			t.Fatalf("missing default acquisition bounds: %+v", options)
		}
		return nil, want
	}})
	if !errors.Is(err, want) {
		t.Fatalf("hook error identity lost: %v", err)
	}
	_, err = plugin.Inspect(context.Background(), source, plugin.ArtifactOptions{PullOCI: func(context.Context, plugin.OCIReference, plugin.ArtifactOptions) (*plugin.Artifact, error) {
		return nil, nil
	}})
	if err == nil || !strings.Contains(err.Error(), "no artifact") {
		t.Fatalf("nil hook result should fail: %v", err)
	}
}

func TestFacadeOCIReference(t *testing.T) {
	ref, err := plugin.ParseOCITag("oci://REGISTRY.example/tools/echo:v1")
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
	parsed, err := plugin.ParseOCI(pinned.String())
	if err != nil || parsed != pinned || parsed.Tag() != "" || ref.Tag() != "v1" {
		t.Fatalf("digest conversion mutated original or lost identity: %s %s %v", ref, parsed, err)
	}
	if _, err := plugin.ParseOCI(ref.String()); err == nil {
		t.Fatal("execution reference accepted an unpinned tag")
	}
}

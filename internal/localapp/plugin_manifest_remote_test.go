package localapp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginManifestRemoteDependencySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	remote := "https://example.test/presets/base.toml?version=1"
	shared := "https://example.test/shared.yaml"
	contents := map[string]string{
		path:   "extends: [\"" + remote + "\"]",
		remote: "extends = [\"../shared.yaml\"]\nplugins = [\"shellcheck@v1.0.0\"]",
		shared: "tools: {}",
	}
	reader := func(_ context.Context, name string) ([]byte, error) {
		if content, ok := contents[name]; ok {
			return []byte(content), nil
		}
		return nil, fmt.Errorf("unexpected source %q", name)
	}
	manifest, err := openPluginManifest(t.Context(), path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.References) != 1 || manifest.References[0].Version() != "v1.0.0" {
		t.Fatalf("references = %v", manifest.References)
	}
	if err := manifest.CheckUnchanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	contents[shared] = "tools: {shellcheck: {enabled: false}}"
	if err := manifest.CheckUnchanged(t.Context()); err == nil || !strings.Contains(err.Error(), shared) {
		t.Fatalf("changed remote error = %v", err)
	}
}

func TestPluginManifestRemoteSourceRejectsLocalRegistry(t *testing.T) {
	remote := "https://example.test/base.yaml"
	_, err := openPluginManifest(t.Context(), filepath.Join(t.TempDir(), "actionlint.yaml"), func(_ context.Context, name string) ([]byte, error) {
		if name == remote {
			return []byte("plugins: [{name: shellcheck, registry: file:///tmp/registry/}]"), nil
		}
		return []byte("extends: [" + remote + "]"), nil
	})
	if err == nil || !strings.Contains(err.Error(), "remote config cannot select a local file registry") || !strings.Contains(err.Error(), remote) {
		t.Fatalf("local registry error = %v", err)
	}
}

func TestPluginManifestRemoteLeafReadOnly(t *testing.T) {
	remote := "HTTPS://EXAMPLE.TEST/actionlint.yaml?version=1"
	reads := 0
	manifest, err := openPluginManifest(t.Context(), remote, func(_ context.Context, name string) ([]byte, error) {
		reads++
		if name != remote {
			return nil, fmt.Errorf("rewritten source %q", name)
		}
		return []byte("plugins: []"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("leaf acquired %d times", reads)
	}
	if err := manifest.CheckUnchanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("remote leaf save = %v", err)
	}
}

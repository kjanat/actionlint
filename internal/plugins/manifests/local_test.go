package manifests_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	inputfiles "actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestReadLocal(t *testing.T) {
	t.Parallel()
	const program = "[]\n"
	sum := sha256.Sum256([]byte(program))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	withDigest := strings.Replace(minimal, `"inline": "[]"`, `"digest": "`+digest+`"`, 1)
	write := func(t *testing.T, files map[string]string) string {
		t.Helper()
		directory := t.TempDir()
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return filepath.Join(directory, "manifest.json")
	}
	reader := inputfiles.New(t.Context(), nil, nil)

	manifest, programs, err := manifests.ReadLocal(reader, write(t, map[string]string{"manifest.json": withDigest, "output.jq": program, "other.jq": "."}))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := programs(digest); !ok || string(got) != program || manifest.Rule.Name != "lint" {
		t.Fatalf("program %q %t manifest %+v", got, ok, manifest)
	}
	for name, files := range map[string]map[string]string{
		"missing program": {"manifest.json": withDigest, "output.jq": "[ ]"},
		"artifacts":       {"manifest.json": strings.Replace(minimal, `"transport": "process",`, `"transport": "process", "artifacts": [{"platform": "linux/amd64", "digest": "`+digest+`", "filename": "lint", "size": 1}],`, 1)},
		"not process":     {"manifest.json": strings.Replace(strings.Replace(minimal, `"transport": "process"`, `"transport": "wasm-core"`, 1), `"process": {`, `"integration": {"tool": "lint", "contract": "x"}, "unused": {`, 1)},
		"invalid":         {"manifest.json": "{}"},
	} {
		if _, _, err := manifests.ReadLocal(reader, write(t, files)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for location, local := range map[string]bool{"./tool/manifest.json": true, `C:\tools\MANIFEST.JSON`: true, "plugin.wasm": false, "https://example.com/manifest.json": false} {
		if manifests.IsLocalManifest(location) != local {
			t.Errorf("IsLocalManifest(%q) != %t", location, local)
		}
	}
}

func TestReadLocalLoadsConfigurationTransforms(t *testing.T) {
	t.Parallel()
	const source = `{options:.config}`
	sum := sha256.Sum256([]byte(source))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	manifestJSON := strings.Replace(minimal, `"schema_version": 1,`, `"schema_version": 1, "configuration_transform":{"language":"jq","digest":"`+digest+`"},`, 1)
	root := t.TempDir()
	path := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(path, []byte(manifestJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := inputfiles.New(t.Context(), nil, nil)
	if _, _, err := manifests.ReadLocal(reader, path); err == nil {
		t.Fatal("missing configuration transform accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "configuration.jq"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, programs, err := manifests.ReadLocal(reader, path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ConfigurationTransform == nil || manifest.ConfigurationTransform.Digest != digest {
		t.Fatal("configuration transform was dropped")
	}
	if got, ok := programs(digest); !ok || string(got) != source {
		t.Fatalf("loaded %q, found=%t", got, ok)
	}
}

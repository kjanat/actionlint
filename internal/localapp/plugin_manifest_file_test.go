package localapp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/pluginref"
)

func manifestTestReference(t *testing.T, name string) pluginref.Reference {
	t.Helper()
	ref, err := pluginref.Parse(name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func writeManifestTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readManifestTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPluginManifestFormats(t *testing.T) {
	ref := manifestTestReference(t, "kjanat/actionlint-plugin-shellcheck@v0.1.1")
	custom, err := pluginref.New("gitlab.com/team/plugin", "v2", "https://example.com/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, source, retained string
	}{
		{"config.yaml", "# intro\nplugins: [old/plugin] # dependencies\n# lint settings\nlint:\n  rules: {unknown-plugin-rule: warn}\n", "# lint settings\nlint:\n  rules: {unknown-plugin-rule: warn}\n"},
		{"config.yml", "plugins:\n  - old/plugin\n\n# retain this\nother: 'quoted'\n", "\n# retain this\nother: 'quoted'\n"},
		{"flow.yaml", "{plugins: [old/plugin], other: kept}\n", "other: kept"},
		{"config.json", "{\n \"plugins\": [\"old/plugin\"],\n \"other\" : {\"kept\":true}\n}\n", "\"other\" : {\"kept\":true}"},
		{"config.toml", "# intro\nplugins = [\n 'old/plugin',\n] # dependencies\n[lint.rules]\nunknown-plugin-rule = 'warn'\n", "# dependencies\n[lint.rules]\nunknown-plugin-rule = 'warn'"},
		{"missing.yaml", "", ""},
		{"missing.json", "", ""},
		{"missing.toml", "", ""},
		{"append.yaml", "# intro\nother: true\n...\n", "# intro\nother: true\n"},
		{"append.json", "{\"other\" : true}\n", "\"other\" : true"},
		{"append.toml", "# intro\n[lint.rules]\nunknown = 'warn'\n", "# intro\n[lint.rules]\nunknown = 'warn'"},
		{"tables.toml", "# intro\n[[plugins]]\nname = 'old/plugin'\n[[plugins]]\nname = 'other/plugin'\n\n# retain this\n[lint.rules]\nunknown = 'warn'\n", "# retain this\n[lint.rules]\nunknown = 'warn'"},
		{"crlf.yaml", "plugins: [old/plugin]\r\n# preserve\r\nother: true\r\n", "# preserve\r\nother: true\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), test.name)
			if test.source != "" {
				writeManifestTest(t, path, test.source)
			}
			manifest, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			refs := []pluginref.Reference{ref, custom}
			if err := manifest.Save(t.Context(), refs); err != nil {
				t.Fatal(err)
			}
			content := readManifestTest(t, path)
			if !strings.Contains(content, test.retained) {
				t.Fatalf("unrelated formatting lost:\n%s", content)
			}
			loaded, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if !loaded.Declared || !loaded.Exists || !slices.Equal(loaded.References, refs) {
				t.Fatalf("unexpected declarations: %#v", loaded)
			}
			if err := manifest.Save(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			empty, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if !empty.Declared || len(empty.References) != 0 {
				t.Fatal("removal must leave explicit empty declarations")
			}
		})
	}
}

func TestPluginManifestInheritance(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	leaf := filepath.Join(dir, "leaf.yml")
	source := "# inherited\nplugins: [base/plugin]\nother: kept\n"
	writeManifestTest(t, base, source)
	writeManifestTest(t, leaf, "extends: [base.yml]\n# leaf\nlint: {rules: {custom-rule: warn}}\n")
	manifest, err := OpenPluginManifest(t.Context(), leaf)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Declared || len(manifest.References) != 1 {
		t.Fatal("inherited declarations missing")
	}
	if err := manifest.Save(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if readManifestTest(t, base) != source {
		t.Fatal("edited base")
	}
	reopened, err := OpenPluginManifest(t.Context(), leaf)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Declared || len(reopened.References) != 0 {
		t.Fatal("empty leaf must replace inherited list")
	}
}

func TestPluginManifestExplicitNull(t *testing.T) {
	dir := t.TempDir()
	writeManifestTest(t, filepath.Join(dir, "base.yml"), "plugins: [base/plugin]\n")
	path := filepath.Join(dir, "leaf.yml")
	writeManifestTest(t, path, "extends: [base.yml]\nplugins: null\n")
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Declared || len(manifest.References) != 0 {
		t.Fatal("null must explicitly disable inherited plugins")
	}
}

func TestPluginManifestConcurrentEdits(t *testing.T) {
	for _, change := range []string{"leaf", "base", "created", "empty-created"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "base.yml")
			path := filepath.Join(dir, "leaf.yml")
			writeManifestTest(t, base, "plugins: [base/plugin]\n")
			if change == "leaf" || change == "base" {
				writeManifestTest(t, path, "extends: [base.yml]\n")
			}
			manifest, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "base":
				writeManifestTest(t, base, "plugins: [changed/plugin]\n")
			case "empty-created":
				writeManifestTest(t, path, "")
			default:
				writeManifestTest(t, path, "# concurrent edit\n")
			}
			before := readManifestTest(t, path)
			if err := manifest.Save(t.Context(), nil); err == nil {
				t.Fatal("expected concurrent-edit error")
			}
			if readManifestTest(t, path) != before {
				t.Fatal("overwrote concurrent edit")
			}
		})
	}
}

func TestPluginManifestRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yml")
	link := filepath.Join(dir, "config.yml")
	writeManifestTest(t, target, "plugins: []\n")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := OpenPluginManifest(t.Context(), link); err == nil {
		t.Fatal("accepted symlink")
	}
	if readManifestTest(t, target) != "plugins: []\n" {
		t.Fatal("changed symlink target")
	}
}

func TestPluginManifestCancellationAndAnchors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	source := "plugins: &dependencies [old/plugin]\nother: *dependencies\n"
	writeManifestTest(t, path, source)
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(t.Context(), nil); err == nil {
		t.Fatal("must not invalidate aliases outside plugins")
	}
	if readManifestTest(t, path) != source {
		t.Fatal("failed edit changed config")
	}
	path = filepath.Join(t.TempDir(), "config.yml")
	writeManifestTest(t, path, "plugins: [old/plugin]\n")
	manifest, err = OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manifest.Save(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if readManifestTest(t, path) != "plugins: [old/plugin]\n" {
		t.Fatal("cancelled edit changed config")
	}
}

func TestPluginManifestRejectsAmbiguousDeclarations(t *testing.T) {
	for _, source := range []string{
		"plugins: []\nplugins: [other/plugin]\n",
		"plugins: [one/plugin, one/plugin]\n",
		"plugins: [{name: one/plugin, unexpected: true}]\n",
		"plugins: not-a-list\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yml")
		writeManifestTest(t, path, source)
		if _, err := OpenPluginManifest(t.Context(), path); err == nil {
			t.Fatalf("accepted invalid config: %s", source)
		}
	}
}

func TestPluginManifestOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, maxPluginManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPluginManifest(t.Context(), path); err == nil {
		t.Fatal("accepted oversized config")
	}
}

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

	"actionlint.kjanat.dev/v2/internal/pluginref"
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
	ref := manifestTestReference(t, "kjanat/actionlint-plugin-shellcheck@v0.2.0")
	custom, err := pluginref.New("gitlab.com/team/plugin", "v2", "https://example.com/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, source, expectedFragment string
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
		{"append-commented-end.yaml", "# intro\nother: true\n... # end\n", "... # end\n"},
		{"append-scalar-ellipsis.yaml", "other: |\n  ... #literal\n  ... ordinary text\n", "  ... #literal\n  ... ordinary text\n"},
		{"replace-commented-end.yaml", "plugins: [old/plugin]\n... # end\n", "... # end\n"},
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
			if !strings.Contains(content, test.expectedFragment) {
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
			if strings.Contains(test.source, "... # end") && !strings.Contains(readManifestTest(t, path), "... # end") {
				t.Fatal("removing plugins discarded commented document end")
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

func TestPluginTOMLInsertionPreservesSchemaHeader(t *testing.T) {
	ref := manifestTestReference(t, "github.com/kjanat/actionlint-plugin-shellcheck@v0.2.0")
	for _, test := range []struct{ name, source, header string }{
		{"schema-only", "#:schema ../actionlint.schema.json\n", "#:schema ../actionlint.schema.json\n"},
		{"schema-without-newline", "#:schema ../actionlint.schema.json", "#:schema ../actionlint.schema.json\n"},
		{"schema-and-table", "#:schema ../actionlint.schema.json\n\n[lint.rules]\npreset = 'none'\n", "#:schema ../actionlint.schema.json\n\n"},
		{"local-actions-and-ignores", "#:schema ../actionlint.schema.json\n\n[local-actions]\n\"./.published-action-test\" = \"./tools/action/testdata/published-action\"\n\n[[ignores]]\nrule = 'missing-local-action'\nfile = '.github/workflows/ci.yml'\nuses = './.actionlint-action-test'\nreason = 'Release bundle built during CI.'\n", "#:schema ../actionlint.schema.json\n\n"},
		{"table-comment", "#:schema ../actionlint.schema.json\n\n# table settings\n[lint.rules]\npreset = 'none'\n", "#:schema ../actionlint.schema.json\n\n"},
		{"intro-crlf", "# project settings\r\n#:schema ../actionlint.schema.json\r\n\r\n[lint.rules]\r\npreset = 'none'\r\n", "# project settings\r\n#:schema ../actionlint.schema.json\r\n\r\n"},
		{"plugin-table", "#:schema ../actionlint.schema.json\n\n[[plugins]]\nname = 'old/plugin'\nversion = 'v1'\n", "#:schema ../actionlint.schema.json\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "actionlint.toml")
			writeManifestTest(t, path, test.source)
			manifest, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := manifest.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
				t.Fatal(err)
			}
			content := readManifestTest(t, path)
			if !strings.HasPrefix(content, test.header) || strings.Count(content, "#:schema ../actionlint.schema.json") != 1 {
				t.Fatalf("plugins moved above schema header:\n%s", content)
			}
			plugin := strings.Index(content, "[[plugins]]")
			if plugin < 0 {
				t.Fatal("new TOML declarations must use plugin tables")
			}
			if !strings.Contains(test.source, "[[plugins]]") {
				if !strings.HasPrefix(content, test.source) {
					t.Fatal("new plugins changed existing document content")
				}
				if table := strings.Index(content, "[lint.rules]"); table >= 0 && plugin < table {
					t.Fatal("new plugins were not appended after existing tables")
				}
			}
			if strings.Contains(test.source, "# table settings") && !strings.Contains(content, "# table settings\n[lint.rules]") {
				t.Fatal("plugin insertion detached the table comment")
			}
			if strings.Contains(test.source, "\r\n") && strings.Contains(strings.ReplaceAll(content, "\r\n", ""), "\n") {
				t.Fatal("plugin insertion introduced LF into CRLF config")
			}
		})
	}
}

func TestPluginTOMLAppendsDirectSourcesAndPreservesExistingPosition(t *testing.T) {
	ref := manifestTestReference(t, "github.com/owner/plugin@v1.2.3")
	direct := manifestTestReference(t, "https://example.org/check.wasm#sha256="+strings.Repeat("a", 64))
	const original = "#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json\n\n[lint.rules]\npreset = 'none'\n"
	path := filepath.Join(t.TempDir(), "actionlint.toml")
	writeManifestTest(t, path, original)
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginref.Reference{ref, direct}
	if err := manifest.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	content := readManifestTest(t, path)
	if !strings.HasPrefix(content, original) || !strings.Contains(content, "source = \"https://example.org/check.wasm#sha256=") {
		t.Fatalf("plugin tables were not appended with pinned direct sources:\n%s", content)
	}
	reloaded, err := OpenPluginManifest(t.Context(), path)
	if err != nil || !slices.Equal(reloaded.References, refs) {
		t.Fatalf("appended declarations changed identity: %+v, %v", reloaded, err)
	}
	const existing = "#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json\n\nplugins = ['old/plugin@v1']\n\n[lint.rules]\npreset = 'none'\n"
	writeManifestTest(t, path, existing)
	manifest, err = OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	content = readManifestTest(t, path)
	if strings.Index(content, "plugins =") != strings.Index(existing, "plugins =") || strings.Contains(content, "[[plugins]]") {
		t.Fatal("existing root declaration moved")
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

func TestPluginManifestDirectoryAlias(t *testing.T) {
	physical, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeManifestTest(t, filepath.Join(physical, "base.yml"), "plugins: [base/plugin]\n")
	writeManifestTest(t, filepath.Join(physical, "config.yml"), "extends: [base.yml]\n")
	for _, name := range []string{"config.yml", filepath.Join("new", "config.yml")} {
		t.Run(name, func(t *testing.T) {
			manifest, err := OpenPluginManifest(t.Context(), filepath.Join(alias, name))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Path != filepath.Join(physical, name) {
				t.Fatalf("manifest kept directory alias: %q", manifest.Path)
			}
			if err := manifest.Save(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenPluginManifest(t.Context(), filepath.Join(physical, name)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginManifestRejectsInheritedDirectoryLink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	writeManifestTest(t, filepath.Join(external, "base.yml"), "plugins: []\n")
	if err := os.Symlink(external, filepath.Join(root, "inherited")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(root, "config.yml")
	writeManifestTest(t, path, "extends: [inherited/base.yml]\n")
	if _, err := OpenPluginManifest(t.Context(), path); err == nil || !strings.Contains(err.Error(), "symlink refused") {
		t.Fatalf("inherited directory link accepted: %v", err)
	}
}

func TestPluginManifestRejectsDirectoryReplacement(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "config")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "actionlint.yml")
	writeManifestTest(t, path, "plugins: []\n")
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := manifest.Save(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "symlink refused") {
		t.Fatalf("replacement directory link accepted: %v", err)
	}
	if got := readManifestTest(t, filepath.Join(moved, "actionlint.yml")); got != "plugins: []\n" {
		t.Fatalf("changed target through directory link: %q", got)
	}
	if _, err := os.Stat(filepath.Join(moved, "actionlint.yml.writing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created guard through directory link: %v", err)
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

package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/pluginregistry"
)

func TestPluginSelectionEmptyDeclarationsAcrossRepositories(t *testing.T) {
	for _, declaration := range []string{"plugins: []\n", "plugins: null\n"} {
		t.Run(strings.TrimSpace(declaration), func(t *testing.T) {
			root, other := pluginRepository(t), pluginRepository(t)
			if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeManifestTest(t, filepath.Join(root, ".github", "actionlint.yaml"), declaration)
			lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(root, ".github", pluginregistry.LockName))
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			input := application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "a.yml"), filepath.Join(other, "b.yml")}}
			selected, err := (PluginSelection{}).Resolve(t.Context(), root, input)
			if err != nil || len(selected.Paths) != 0 || selected.LockFile != "" {
				t.Fatalf("empty selection: %+v, %v", selected, err)
			}
			if err := selected.ValidateScope(input.Paths); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginSelectionAllowsPluginFreeLinkedConfig(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "inherited"}[inherited], func(t *testing.T) {
			root := pluginRepository(t)
			directory := filepath.Join(root, ".github")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "actionlint.yaml")
			target := filepath.Join(t.TempDir(), "shared.yaml")
			writeManifestTest(t, target, "plugins: []\n")
			link := path
			if inherited {
				link = filepath.Join(directory, "base.yaml")
				writeManifestTest(t, path, "extends: [base.yaml]\n")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, selection := range []PluginSelection{{}, {ConfigFile: path}} {
				selected, err := selection.Resolve(t.Context(), root, application.Input{Kind: application.Repository})
				if err != nil || len(selected.Paths) != 0 {
					t.Fatalf("plugin-free linked config: %+v, %v", selected, err)
				}
			}
			if _, err := OpenPluginProject(t.Context(), root, "", ""); err == nil {
				t.Fatal("mutation accepted linked configuration")
			}
			writeManifestTest(t, target, "plugins: [publisher/plugin]\n")
			if _, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Repository}); err == nil || !strings.Contains(err.Error(), "symlink refused") {
				t.Fatalf("plugin activation accepted linked configuration: %v", err)
			}
		})
	}
}

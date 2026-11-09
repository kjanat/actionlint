package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
)

func TestPluginSelectionDirectSourcesKeepRepositoryScope(t *testing.T) {
	root, other := pluginRepository(t), pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	source := "https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32)
	writeManifestTest(t, path, "plugins: ['"+source+"']\n")
	input := application.Input{Kind: application.Repository}
	selected, err := (PluginSelection{}).Select(t.Context(), root, input)
	if err != nil || len(selected.Paths) != 1 || selected.Paths[0] != source || selected.LockFile != "" {
		t.Fatalf("direct selection: %+v, %v", selected, err)
	}
	foreign := []string{filepath.Join(other, "workflow.yml")}
	if err := selected.ValidateScope(t.Context(), foreign); err == nil {
		t.Fatal("automatic direct plugin escaped repository scope")
	}
	empty, err := (PluginSelection{}).Select(t.Context(), other, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.ValidateScope(t.Context(), []string{filepath.Join(root, "workflow.yml")}); err == nil {
		t.Fatal("plugin-free session accepted foreign direct plugin")
	}
	input = application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "a.yml"), foreign[0]}}
	if _, err := (PluginSelection{}).Select(t.Context(), root, input); err == nil {
		t.Fatal("mixed repositories accepted automatic direct plugin")
	}
	explicit, err := (PluginSelection{ConfigFile: path}).Select(t.Context(), root, input)
	if err != nil || len(explicit.Paths) != 1 {
		t.Fatalf("explicit direct selection: %+v, %v", explicit, err)
	}
	if err := explicit.ValidateScope(t.Context(), foreign); err != nil {
		t.Fatal(err)
	}
}

func TestPluginSelectionOverlaysDetermineLinkedActivation(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "inherited"}[inherited], func(t *testing.T) {
			root, other := pluginRepository(t), pluginRepository(t)
			path := filepath.Join(root, ".github", "actionlint.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			source := "https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32)
			target := filepath.Join(t.TempDir(), "shared.yaml")
			writeManifestTest(t, target, "plugins: ['"+source+"']\n")
			link := path
			if inherited {
				link = filepath.Join(filepath.Dir(path), "base.yaml")
				writeManifestTest(t, path, "extends: [base.yaml]\n")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, value := range []string{"plugins: []", "plugins: null"} {
				overlay, err := config.ParseOverlay("config", []byte(value))
				if err != nil {
					t.Fatal(err)
				}
				selection := PluginSelection{Overlays: []config.Overlay{overlay}}
				input := application.Input{Kind: application.Repository}
				for _, explicit := range []string{"", path} {
					selection.ConfigFile = explicit
					selected, err := selection.Select(t.Context(), root, input)
					if err != nil || len(selected.Paths) != 0 {
						t.Fatalf("overlay failed to disable linked plugin: %+v, %v", selected, err)
					}
				}
				selection.ConfigFile = ""
				selected, err := selection.Select(t.Context(), other, input)
				if err != nil {
					t.Fatal(err)
				}
				if err := selected.ValidateScope(t.Context(), []string{filepath.Join(root, "workflow.yml")}); err != nil {
					t.Fatalf("foreign scope ignored disabling overlay: %v", err)
				}
			}
			for _, mode := range []pluginSelectionMode{pluginAnalysis, pluginRuleDiscovery} {
				if _, err := (PluginSelection{}).selectSources(t.Context(), root, application.Input{Kind: application.Repository}, mode); err == nil || !strings.Contains(err.Error(), "symlink refused") {
					t.Fatalf("active direct plugin accepted linked config: %v", err)
				}
			}
			writeManifestTest(t, target, "plugins: []\n")
			overlay, err := config.ParseOverlay("config", []byte("plugins: ['"+source+"']"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (PluginSelection{Overlays: []config.Overlay{overlay}}).Select(t.Context(), root, application.Input{Kind: application.Repository}); err == nil || !strings.Contains(err.Error(), "symlink refused") {
				t.Fatalf("overlay activated plugin through linked config: %v", err)
			}
		})
	}
}

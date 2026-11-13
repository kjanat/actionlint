package installation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
)

func TestPluginSelectionDirectSourcesKeepRepositoryScope(t *testing.T) {
	t.Parallel()
	root, other := repositoryfixture.Directory(t), repositoryfixture.Directory(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	source := "https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32)
	fixturefiles.WriteText(t, path, "plugins: ['"+source+"']\n")
	input := inputmodel.Request{Kind: inputmodel.Repository}
	selected, err := (Selection{RuleSet: (config.RuleSet{})}).Select(t.Context(), root, input)
	if err != nil || len(selected.Paths) != 1 || selected.Paths[0] != source || selected.LockFile != "" {
		t.Fatalf("direct selection: %+v, %v", selected, err)
	}
	foreign := []string{filepath.Join(other, "workflow.yml")}
	if err := selected.ValidateScope(t.Context(), config.RuleSet{}, foreign); err == nil {
		t.Fatal("automatic direct plugin escaped repository scope")
	}
	empty, err := (Selection{RuleSet: (config.RuleSet{})}).Select(t.Context(), other, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.ValidateScope(t.Context(), config.RuleSet{}, []string{filepath.Join(root, "workflow.yml")}); err == nil {
		t.Fatal("plugin-free session accepted foreign direct plugin")
	}
	input = inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join(root, "a.yml"), foreign[0]}}
	if _, err := (Selection{RuleSet: (config.RuleSet{})}).Select(t.Context(), root, input); err == nil {
		t.Fatal("mixed repositories accepted automatic direct plugin")
	}
	explicit, err := (Selection{ConfigFile: path, RuleSet: (config.RuleSet{})}).Select(t.Context(), root, input)
	if err != nil || len(explicit.Paths) != 1 {
		t.Fatalf("explicit direct selection: %+v, %v", explicit, err)
	}
	if err := explicit.ValidateScope(t.Context(), config.RuleSet{}, foreign); err != nil {
		t.Fatal(err)
	}
}

func TestPluginSelectionOverlaysDetermineLinkedActivation(t *testing.T) {
	t.Parallel()
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "inherited"}[inherited], func(t *testing.T) {
			t.Parallel()
			root, other := repositoryfixture.Directory(t), repositoryfixture.Directory(t)
			path := filepath.Join(root, ".github", "actionlint.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			source := "https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32)
			target := filepath.Join(t.TempDir(), "shared.yaml")
			fixturefiles.WriteText(t, target, "plugins: ['"+source+"']\n")
			link := path
			if inherited {
				link = filepath.Join(filepath.Dir(path), "base.yaml")
				fixturefiles.WriteText(t, path, "extends: [base.yaml]\n")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, value := range []string{"plugins: []", "plugins: null"} {
				overlay, err := (config.RuleSet{}).ParseOverlay("config", []byte(value))
				if err != nil {
					t.Fatal(err)
				}
				selection := Selection{Overlays: []config.Overlay{overlay}, RuleSet: (config.RuleSet{})}
				input := inputmodel.Request{Kind: inputmodel.Repository}
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
				if err := selected.ValidateScope(t.Context(), config.RuleSet{}, []string{filepath.Join(root, "workflow.yml")}); err != nil {
					t.Fatalf("foreign scope ignored disabling overlay: %v", err)
				}
			}
			for _, mode := range []configplugins.ReadMode{configplugins.CompleteConfiguration, configplugins.PluginDeclarationsOnly} {
				if _, err := (Selection{RuleSet: (config.RuleSet{})}).selectSources(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}, mode); err == nil || !strings.Contains(err.Error(), "symlink refused") {
					t.Fatalf("active direct plugin accepted linked config: %v", err)
				}
			}
			fixturefiles.WriteText(t, target, "plugins: []\n")
			overlay, err := (config.RuleSet{}).ParseOverlay("config", []byte("plugins: ['"+source+"']"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (Selection{Overlays: []config.Overlay{overlay}, RuleSet: (config.RuleSet{})}).Select(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}); err == nil || !strings.Contains(err.Error(), "symlink refused") {
				t.Fatalf("overlay activated plugin through linked config: %v", err)
			}
		})
	}
}

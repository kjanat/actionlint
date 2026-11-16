package checking_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestPluginSelectionEmptyDeclarationsAcrossRepositories(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{"plugins: []\n", "plugins: null\n"} {
		t.Run(strings.TrimSpace(declaration), func(t *testing.T) {
			t.Parallel()
			root, other := repositoryfixture.Directory(t), repositoryfixture.Directory(t)
			if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
				t.Fatal(err)
			}
			fixturefiles.WriteText(t, filepath.Join(root, ".github", "actionlint.yaml"), declaration)
			input := checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, "a.yml"), filepath.Join(other, "b.yml")}}
			selected, err := (pluginselection.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, input)
			if err != nil || len(selected.Paths) != 0 {
				t.Fatalf("empty selection: %+v, %v", selected, err)
			}
			if err := selected.ValidateScope(t.Context(), checking.ConfigurationRules(), input.Paths); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginSelectionAllowsPluginFreeLinkedConfig(t *testing.T) {
	t.Parallel()
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "inherited"}[inherited], func(t *testing.T) {
			t.Parallel()
			root := repositoryfixture.Directory(t)
			directory := filepath.Join(root, ".github")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "actionlint.yaml")
			target := filepath.Join(t.TempDir(), "shared.yaml")
			fixturefiles.WriteText(t, target, "plugins: []\n")
			link := path
			if inherited {
				link = filepath.Join(directory, "base.yaml")
				fixturefiles.WriteText(t, path, "extends: [base.yaml]\n")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, selection := range []pluginselection.Selection{{}, {ConfigFile: path}} {
				selected, err := selection.Resolve(t.Context(), root, checkrequest.Request{Kind: checkrequest.Repository})
				if err != nil || len(selected.Paths) != 0 {
					t.Fatalf("plugin-free linked config: %+v, %v", selected, err)
				}
			}
			if _, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), pluginacquisition.Settings{}, root, ""); err == nil {
				t.Fatal("mutation accepted linked configuration")
			}
			fixturefiles.WriteText(t, target, "plugins: [publisher/plugin]\n")
			if _, err := (pluginselection.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, checkrequest.Request{Kind: checkrequest.Repository}); err == nil || !strings.Contains(err.Error(), "symlink refused") {
				t.Fatalf("plugin activation accepted linked configuration: %v", err)
			}
		})
	}
}

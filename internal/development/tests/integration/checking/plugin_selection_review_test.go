package checking_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginSelectionRetainsObservedLocksAcrossRepositories(t *testing.T) {
	t.Parallel()
	roots := []string{repositoryfixture.Directory(t), repositoryfixture.Directory(t)}
	var paths, locks []string
	for _, root := range roots {
		path := filepath.Join(root, ".github", plugininstall.LockName)
		lock, err := plugininstall.OpenLock(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := lock.Save(t.Context()); err != nil {
			t.Fatal(err)
		}
		locks = append(locks, lock.Path)
		paths = append(paths, filepath.Join(root, "workflow.yml"))
	}
	selected, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), roots[0], inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Paths) != 0 || selected.LockFile != "" || !slices.Equal(selected.Inputs, locks) {
		t.Fatalf("observed empty locks changed activation: %+v, want inputs %v", selected, locks)
	}
	if err := selected.ValidateScope(t.Context(), checking.ConfigurationRules(), paths); err != nil {
		t.Fatalf("empty lock restricted scope: %v", err)
	}
}

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
			lock, err := plugininstall.OpenLock(t.Context(), filepath.Join(root, ".github", plugininstall.LockName))
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join(root, "a.yml"), filepath.Join(other, "b.yml")}}
			selected, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, input)
			if err != nil || len(selected.Paths) != 0 || selected.LockFile != "" {
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
			for _, selection := range []plugininstall.Selection{{}, {ConfigFile: path}} {
				selected, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
				if err != nil || len(selected.Paths) != 0 {
					t.Fatalf("plugin-free linked config: %+v, %v", selected, err)
				}
			}
			if _, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), plugininstall.Acquisition{}, root, "", ""); err == nil {
				t.Fatal("mutation accepted linked configuration")
			}
			fixturefiles.WriteText(t, target, "plugins: [publisher/plugin]\n")
			if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}); err == nil || !strings.Contains(err.Error(), "symlink refused") {
				t.Fatalf("plugin activation accepted linked configuration: %v", err)
			}
		})
	}
}

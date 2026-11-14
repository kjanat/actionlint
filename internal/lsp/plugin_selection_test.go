package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"

	"actionlint.kjanat.dev/v2/internal/checking"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestDirectoryPluginSelectionUsesDirectoryRepository(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte("plugins: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".", root, filepath.Join(root, ".github")} {
		if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Select(t.Context(), root, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}}); err == nil {
			t.Fatalf("directory %q skipped its repository plugin configuration", path)
		}
	}
}

func TestStdinPluginSelectionMatchesProjectDiscovery(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.WriteFile(config, []byte("plugins: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "<stdin>", "missing.yml", filepath.Join(root, "missing.yml")} {
		selected, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Stdin, Name: name})
		if err != nil || len(selected.Paths) != 0 {
			t.Fatalf("stdin %q without a project selected plugins: %+v, %v", name, selected, err)
		}
	}
	path := filepath.Join(root, "workflow.yml")
	if err := os.WriteFile(path, []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"workflow.yml", path} {
		if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Stdin, Name: name}); err == nil {
			t.Fatalf("existing stdin %q did not load its project configuration", name)
		}
	}
	if _, err := (plugininstall.Selection{ConfigFile: config, RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Stdin}); err == nil {
		t.Fatal("explicit configuration ignored for unnamed stdin")
	}
}

func TestPluginSelectionScopes(t *testing.T) {
	t.Parallel()
	root, other := repositoryfixture.Directory(t), repositoryfixture.Directory(t)
	input := inputmodel.Request{Kind: inputmodel.Repository}
	empty, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, ".github")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(other, "workflow.yml")
	if err := empty.ValidateScope(t.Context(), checking.ConfigurationRules(), []string{foreign}); err != nil {
		t.Fatalf("plugin-free repository restricted scope: %v", err)
	}
	selected, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), other, input)
	if err != nil {
		t.Fatalf("selection: %+v %v", selected, err)
	}
	if err := selected.ValidateScope(t.Context(), checking.ConfigurationRules(), []string{filepath.Join(root, "workflow.yml")}); err != nil {
		t.Fatalf("empty selection restricted scope: %v", err)
	}
	mixed := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{foreign, filepath.Join(root, "workflow.yml")}}
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, mixed); err != nil {
		t.Fatalf("plugin-free repositories rejected: %v", err)
	}
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, s := range []plugininstall.Selection{{Disabled: true}, {Paths: []string{"explicit.wasm"}}, {ConfigFile: configPath}} {
		got, err := s.Resolve(t.Context(), other, mixed)
		if err != nil {
			t.Fatalf("override: %+v %v", got, err)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), alias, input); err != nil {
		t.Fatalf("checkout alias: %v", err)
	}
	if err := os.Symlink(path, filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), root, input); err == nil {
		t.Fatal("symlinked .github accepted")
	}
	// A target containing .git must not become the trusted boundary.
	third := repositoryfixture.Directory(t)
	if err := os.Symlink(other, filepath.Join(third, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), plugininstall.Acquisition{}, third, ""); err == nil {
		t.Fatal("installation followed .github into another repository")
	}
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules()}).Resolve(t.Context(), third, input); err == nil {
		t.Fatal("automatic selection followed .github into another repository")
	}
}

func TestManagedPluginEditorSnapshot(t *testing.T) {
	t.Parallel()
	wasm, err := os.ReadFile("../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
	r.Add(t, "v1.0.0", wasm)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(pin.Name, pin.Version, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	selected, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		t.Fatal(err)
	}
	other := repositoryfixture.Directory(t)
	foreign := filepath.Join(other, "workflow.yml")
	if err := selected.ValidateScope(t.Context(), checking.ConfigurationRules(), []string{foreign}); err == nil {
		t.Fatal("active plugins escaped their repository")
	}
	empty, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).Resolve(t.Context(), other, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.ValidateScope(t.Context(), checking.ConfigurationRules(), []string{filepath.Join(root, "workflow.yml")}); err == nil {
		t.Fatal("foreign active plugins were ignored")
	}
	mixed := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{foreign, filepath.Join(root, "workflow.yml")}}
	if _, err := (plugininstall.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).Resolve(t.Context(), root, mixed); err == nil {
		t.Fatal("mixed active plugin scope accepted")
	}
	session, err := newEditorSession(t.Context(), checking.Options{WorkingDir: root}, selected.Paths, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if err := project.Save(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	r.SetOffline(true)
	for range 2 {
		result, err := analyzeDocuments(t.Context(), session.options, map[string]Document{filepath.Join(root, "workflow.yml"): {Text: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\npermissions: {}\n"}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range result.Diagnostics {
			if d.Rule == "no-sudo" && strings.Contains(d.Message, "sudo") {
				found = true
			}
		}
		if !found {
			t.Fatalf("snapshot lost: %+v", result.Diagnostics)
		}
	}
}

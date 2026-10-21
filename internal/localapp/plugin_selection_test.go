package localapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/langserver"
	"actionlint.kjanat.dev/internal/pluginref"
	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/testutil"
)

func pluginRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestStdinPluginSelectionMatchesProjectDiscovery(t *testing.T) {
	root := pluginRepository(t)
	if err := os.Mkdir(filepath.Join(root, ".github"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.WriteFile(config, []byte("plugins: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "<stdin>", "missing.yml", filepath.Join(root, "missing.yml")} {
		selected, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Stdin, Name: name})
		if err != nil || len(selected.Paths) != 0 || selected.LockFile != "" {
			t.Fatalf("stdin %q without a project selected plugins: %+v, %v", name, selected, err)
		}
	}
	path := filepath.Join(root, "workflow.yml")
	if err := os.WriteFile(path, []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"workflow.yml", path} {
		if _, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Stdin, Name: name}); err == nil {
			t.Fatalf("existing stdin %q did not load its project configuration", name)
		}
	}
	if _, err := (PluginSelection{ConfigFile: config}).Resolve(t.Context(), root, application.Input{Kind: application.Stdin}); err == nil {
		t.Fatal("explicit configuration ignored for unnamed stdin")
	}
}

func TestPluginSelectionScopes(t *testing.T) {
	root, other := pluginRepository(t), pluginRepository(t)
	input := application.Input{Kind: application.Repository}
	empty, err := (PluginSelection{}).Resolve(t.Context(), root, input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, ".github", pluginregistry.LockName)
	f, err := pluginregistry.OpenLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(other, "workflow.yml")
	if err := empty.ValidateScope([]string{foreign}); err != nil {
		t.Fatalf("empty foreign lock restricted scope: %v", err)
	}
	selected, err := (PluginSelection{}).Resolve(t.Context(), other, input)
	if err != nil || selected.LockFile != "" {
		t.Fatalf("selection: %+v %v", selected, err)
	}
	if err := selected.ValidateScope([]string{filepath.Join(root, "workflow.yml")}); err != nil {
		t.Fatalf("empty selection restricted scope: %v", err)
	}
	mixed := application.Input{Kind: application.Files, Paths: []string{foreign, filepath.Join(root, "workflow.yml")}}
	if _, err := (PluginSelection{}).Resolve(t.Context(), root, mixed); err != nil {
		t.Fatalf("plugin-free repositories rejected: %v", err)
	}
	for _, s := range []PluginSelection{{Disabled: true}, {Paths: []string{"explicit.wasm"}}, {ConfigFile: filepath.Join(root, "config.yaml")}} {
		got, err := s.Resolve(t.Context(), other, mixed)
		if err != nil || got.LockFile != "" {
			t.Fatalf("override: %+v %v", got, err)
		}
	}
	got, err := (PluginSelection{Disabled: true, LockFile: path}).Resolve(t.Context(), root, mixed)
	if err != nil || got.LockFile == "" {
		t.Fatalf("explicit lock: %+v %v", got, err)
	}
	if _, err := (PluginSelection{Paths: []string{"explicit.wasm"}, LockFile: path}).Resolve(t.Context(), root, input); err == nil {
		t.Fatal("ambiguous selection accepted")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := (PluginSelection{}).Resolve(t.Context(), alias, input); err != nil {
		t.Fatalf("checkout alias without lock: %v", err)
	}
	if err := os.Symlink(filepath.Dir(path), filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := (PluginSelection{}).Resolve(t.Context(), root, input); err == nil {
		t.Fatal("symlinked .github accepted")
	}
	// A target containing .git must not become the trusted boundary.
	third := pluginRepository(t)
	if err := os.Symlink(other, filepath.Join(third, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPluginLock(t.Context(), third, "", ""); err == nil {
		t.Fatal("installation followed .github into another repository")
	}
	if _, err := (PluginSelection{}).Resolve(t.Context(), third, input); err == nil {
		t.Fatal("automatic selection followed .github into another repository")
	}
}

func TestManagedPluginEditorSnapshot(t *testing.T) {
	wasm, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", wasm)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	f, err := pluginregistry.OpenLock(t.Context(), filepath.Join(root, ".github", pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(pin.Name, pin.Selector, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	selected, err := (PluginSelection{}).Resolve(t.Context(), root, application.Input{Kind: application.Repository})
	if err != nil {
		t.Fatal(err)
	}
	other := pluginRepository(t)
	foreign := filepath.Join(other, "workflow.yml")
	if err := selected.ValidateScope([]string{foreign}); err == nil {
		t.Fatal("active plugins escaped their repository")
	}
	empty, err := (PluginSelection{}).Resolve(t.Context(), other, application.Input{Kind: application.Repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.ValidateScope([]string{filepath.Join(root, "workflow.yml")}); err == nil {
		t.Fatal("foreign active plugins were ignored")
	}
	mixed := application.Input{Kind: application.Files, Paths: []string{foreign, filepath.Join(root, "workflow.yml")}}
	if _, err := (PluginSelection{}).Resolve(t.Context(), root, mixed); err == nil {
		t.Fatal("mixed active plugin scope accepted")
	}
	session, err := newEditorSession(t.Context(), application.Options{WorkingDir: root}, selected.Paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if err := f.Remove(pin.Name); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.Offline = true
	for range 2 {
		result, err := analyzeDocuments(t.Context(), session.options, map[string]langserver.Document{filepath.Join(root, "workflow.yml"): {Text: "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo make install}]\n"}})
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

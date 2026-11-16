package checking_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"

	"actionlint.kjanat.dev/v2/internal/input/configuration/plugins"

	"actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginConfigDiscoverySkipsDanglingLinks(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	directory := filepath.Join(root, ".github")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "missing.yaml"), filepath.Join(directory, "actionlint.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(directory, "actionlint.yml")
	fixturefiles.WriteText(t, path, "plugins: []\n")
	project, err := installation.OpenProject(t.Context(), checking.ConfigurationRules(), dependencycache.Acquisition{}, root, "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(project.Manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(got, want) || !project.Manifest.Declared {
		t.Fatalf("lower-priority declarations ignored: %+v", project.Manifest)
	}
}

func TestPluginConfigDiscoveryPreservesLookupErrors(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fixturefiles.WriteText(t, filepath.Join(directory, "actionlint.yml"), "plugins: []\n")
	if _, err := plugins.FindOrDefaultPath(directory); err == nil {
		t.Fatal("configuration symlink loop ignored")
	}
}

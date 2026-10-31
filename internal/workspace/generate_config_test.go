package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestGenerateConfigActionOnlyRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeToolFixture(t, root, "action.yml", "name: example\ndescription: example\nruns: {using: composite, steps: []}\n")
	project, err := workspace.Discoverer(false).At(root)
	if err != nil {
		t.Fatal(err)
	}
	if project == nil {
		t.Fatal("action-only repository not found")
	}
	path, err := repoconfig.Generate(t.Context(), project.RootDir(), false, "test")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoconfig.Parse(content); err != nil {
		t.Fatalf("invalid generated config: %v", err)
	}
	if _, err := repoconfig.Generate(t.Context(), project.RootDir(), false, "test"); err == nil {
		t.Fatal("existing config overwritten")
	}
}

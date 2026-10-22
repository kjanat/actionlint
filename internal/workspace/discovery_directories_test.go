package workspace_test

import (
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestWorkflowDiscoveryRecordsListedDirectories(t *testing.T) {
	root := t.TempDir()
	writeToolRepository(t, root)
	writeToolFixture(t, root, ".github/workflows/ci.yml", toolWorkflow)
	writeToolFixture(t, root, ".github/workflows/nested/more.yml", toolWorkflow)
	workflows := filepath.Join(root, ".github", "workflows")
	session, err := workspace.New(workspace.Options{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := session.Repository("")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := session.Directory(workflows, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]*workspace.Selection{"repository": repository, "directory": directory} {
		for _, dir := range []string{workflows, filepath.Join(workflows, "nested")} {
			if !slices.Contains(result.Inputs, dir) {
				t.Errorf("%s inputs %q lack the listed directory %q", name, result.Inputs, dir)
			}
		}
	}
}

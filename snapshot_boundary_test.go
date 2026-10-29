package actionlint

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalMetadataRejectsRepositoryEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repository")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "action.yml"), []byte("name: Outside\ndescription: Outside\nruns:\n  using: composite\n  steps: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "workflow.yml"), []byte("on: workflow_call\njobs: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := NewProject(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _, err := NewLocalActionsCache(project, io.Discard).FindMetadata("./..")
	if metadata != nil || err == nil || !strings.Contains(err.Error(), "escapes the repository") {
		t.Fatalf("outside action was read: %+v, %v", metadata, err)
	}
	reusable, err := NewLocalReusableWorkflowCache(project, root, io.Discard).FindMetadata("./../workflow.yml")
	if reusable != nil || err == nil || !strings.Contains(err.Error(), "escapes the repository") {
		t.Fatalf("outside workflow was read: %+v, %v", reusable, err)
	}
}

func TestLocalActionRunsFileRejectsRepositoryEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repository")
	if err := os.MkdirAll(filepath.Join(root, "local"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "host.js"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "local", "action.yml"), []byte("name: local\ndescription: local\nruns:\n  using: node24\n  main: ../../host.js\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := NewProject(root)
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := linter.Lint(".github/workflows/test.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n"), project)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if strings.Contains(finding.Message, "escapes the repository") {
			return
		}
	}
	t.Fatalf("outside action entrypoint accepted: %v", findings)
}

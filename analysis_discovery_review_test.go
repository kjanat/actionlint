package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRepositoryRetainsExcludedCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['**/excluded.yml']}\n")
	excluded := writeShellcheckFixture(t, root, ".github/workflows/excluded.yml", "invalid: [yaml\n")
	selected := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
	var notified []string
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, OnFilesSelected: func(paths []string) { notified = paths }, ReadFile: func(path string) ([]byte, error) {
		if path == excluded {
			t.Error("excluded workflow was read")
		}
		return os.ReadFile(path)
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Repository("")
	if err != nil || result.FileCount() != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("excluded workflow affected analysis: %+v, %v", result, err)
	}
	if !slices.Contains(result.Inputs, excluded) || !slices.Contains(result.Inputs, selected) {
		t.Fatalf("discovered candidates missing from protected inputs: %v", result.Inputs)
	}
	if !slices.Equal(notified, []string{selected, excluded}) {
		t.Fatalf("discovery callback lost candidate paths: %v", notified)
	}
}

func TestRelativeProjectKeepsInMemoryWorkflowMetadata(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	project, err := NewProject(".")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("on: workflow_call\njobs:\n  recurse:\n    uses: ./.github/workflows/memory.yml\n")
	for _, cwd := range []string{".", root} {
		result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: cwd, Sources: []SourceUnit{{Path: ".github/workflows/memory.yml", Content: content, Project: project}}, ReadFile: func(path string) ([]byte, error) {
			t.Errorf("in-memory workflow metadata was read from disk: %s", path)
			return nil, os.ErrNotExist
		}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("in-memory metadata lost for working directory %q: %+v, %v", cwd, result, err)
		}
	}
	if project.RootDir() != "." {
		t.Fatalf("caller-owned project was mutated: %q", project.RootDir())
	}
}

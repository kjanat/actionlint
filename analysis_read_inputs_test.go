package actionlint

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestWorkflowDiscoveryRecordsListedDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
	writeShellcheckFixture(t, root, ".github/workflows/nested/more.yml", commandGoodWorkflow)
	workflows := filepath.Join(root, ".github", "workflows")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := session.Repository("")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := session.directory(workflows, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]*AnalysisResult{"repository": repository, "directory": directory} {
		for _, dir := range []string{workflows, filepath.Join(workflows, "nested")} {
			if !slices.Contains(result.Inputs, dir) {
				t.Errorf("%s inputs %q lack the listed directory %q", name, result.Inputs, dir)
			}
		}
	}
}

func TestWorkflowNamesRecordOnlyReadSiblings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot make a file unreadable on Windows")
	}
	root := t.TempDir()
	consumer := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	build := writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+commandGoodWorkflow)
	locked := writeShellcheckFixture(t, root, ".github/workflows/locked.yml", "name: Locked\n"+commandGoodWorkflow)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	if data, err := os.ReadFile(locked); err == nil {
		t.Skipf("the file stays readable, as for root: %q", data)
	}
	dir := filepath.Join(root, ".github", "workflows")
	request := AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(dir, "consumer.yml"), Content: []byte(consumer), Project: &Project{root: root}}}}
	result, err := Analyze(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Inputs, build) || slices.Contains(result.Inputs, locked) {
		t.Fatalf("inputs %q, want %q and not the unreadable %q", result.Inputs, build, locked)
	}
}

func TestCaseInsensitiveRepositoryPathRecordsListings(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	var listed []string
	local, err := caseInsensitiveRepositoryPathChecked(root, "./SCRIPTS/nested", func(dir string) { listed = append(listed, dir) })
	if err != nil || local != "./scripts/Nested" {
		t.Fatalf("resolved %q, %v", local, err)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("listed %q, want %q", listed, want)
	}
	listed = nil
	if _, err := caseInsensitiveRepositoryPathChecked(filepath.Join(root, "absent"), "./x", func(dir string) { listed = append(listed, dir) }); err == nil || listed != nil {
		t.Fatalf("absent root resolved with %v, listed %q", err, listed)
	}
	paths := runPaths{workspace: root, analysis: root, caseInsensitive: true, onRead: func(dir string) { listed = append(listed, dir) }}
	if mapped, known := paths.analysisPath("SCRIPTS/nested"); !known || mapped != filepath.Join(root, "scripts", "Nested") {
		t.Fatalf("analysisPath = %q, %v", mapped, known)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("analysisPath listed %q, want %q", listed, want)
	}
}

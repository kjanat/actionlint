package workflow_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/workflow/execution"
	"actionlint.kjanat.dev/internal/workspace"
)

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
	build := testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
	locked := testutil.WriteFixture(t, root, ".github/workflows/locked.yml", "name: Locked\n"+testutil.GoodWorkflow)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	if data, err := os.ReadFile(locked); err == nil {
		t.Skipf("the file stays readable, as for root: %q", data)
	}
	dir := filepath.Join(root, ".github", "workflows")
	request := application.Options{WorkingDir: root}
	requestSources := []workspace.Source{{Path: filepath.Join(dir, "consumer.yml"), Content: []byte(consumer), Project: workspace.Identity(root)}}
	result, err := application.Analyze(t.Context(), request, requestSources)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Inputs, build) || slices.Contains(result.Inputs, locked) {
		t.Fatalf("inputs %q, want %q and not the unreadable %q", result.Inputs, build, locked)
	}
}

func TestCaseInsensitiveRepositoryPathRecordsListings(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	var listed []string
	local, err := execution.CaseInsensitiveRepositoryPath(root, "./SCRIPTS/nested", fsread.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) }))
	if err != nil || local != "./scripts/Nested" {
		t.Fatalf("resolved %q, %v", local, err)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("listed %q, want %q", listed, want)
	}
	listed = nil
	if _, err := execution.CaseInsensitiveRepositoryPath(filepath.Join(root, "absent"), "./x", fsread.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) })); err == nil || listed != nil {
		t.Fatalf("absent root resolved with %v, listed %q", err, listed)
	}
	paths := execution.Paths{Workspace: root, Analysis: root, CaseInsensitive: true, Reader: fsread.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) })}
	if mapped, known := paths.AnalysisPath("SCRIPTS/nested"); !known || mapped != filepath.Join(root, "scripts", "Nested") {
		t.Fatalf("analysisPath = %q, %v", mapped, known)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("analysisPath listed %q, want %q", listed, want)
	}
}

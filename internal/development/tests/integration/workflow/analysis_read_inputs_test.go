package workflow_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files/runnerpaths"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestWorkflowNamesRecordOnlyReadSiblings(t *testing.T) {
	t.Parallel()
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
	build := fixturefiles.Write(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
	locked := fixturefiles.Write(t, root, ".github/workflows/locked.yml", "name: Locked\n"+testutil.GoodWorkflow)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	if data, err := os.ReadFile(locked); err == nil {
		t.Skipf("the file stays readable, as for root: %q", data)
	}
	dir := filepath.Join(root, ".github", "workflows")
	request := checking.Options{WorkingDir: root}
	requestSources := []workspace.Source{{Path: filepath.Join(dir, "consumer.yml"), Content: []byte(consumer), Project: workspace.Identity(root)}}
	result, err := checking.Analyze(t.Context(), request, requestSources)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Inputs, build) || slices.Contains(result.Inputs, locked) {
		t.Fatalf("inputs %q, want %q and not the unreadable %q", result.Inputs, build, locked)
	}
}

func TestCaseInsensitiveRepositoryPathRecordsListings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixturefiles.Write(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	var listed []string
	local, err := runnerpaths.CaseInsensitiveRepositoryPath(root, "./SCRIPTS/nested", files.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) }))
	if err != nil || local != "./scripts/Nested" {
		t.Fatalf("resolved %q, %v", local, err)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("listed %q, want %q", listed, want)
	}
	listed = nil
	if _, err := runnerpaths.CaseInsensitiveRepositoryPath(filepath.Join(root, "absent"), "./x", files.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) })); err == nil || listed != nil {
		t.Fatalf("absent root resolved with %v, listed %q", err, listed)
	}
	paths := execution.Paths{Workspace: root, Analysis: root, CaseInsensitive: true}
	reader := files.New(t.Context(), nil, func(dir string) { listed = append(listed, dir) })
	if mapped, known := runnerpaths.AnalysisPath(paths, "SCRIPTS/nested", reader); !known || mapped != filepath.Join(root, "scripts", "Nested") {
		t.Fatalf("analysisPath = %q, %v", mapped, known)
	}
	if want := []string{root, filepath.Join(root, "scripts")}; !slices.Equal(listed, want) {
		t.Fatalf("analysisPath listed %q, want %q", listed, want)
	}
}

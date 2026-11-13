package checking_test

import (
	"os"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestRepositoryRetainsExcludedCandidates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGitRepository(t, root)
	writeFixture(t, root, ".github/actionlint.yaml", "files: {excludes: ['**/excluded.yml']}\n")
	excluded := writeFixture(t, root, ".github/workflows/excluded.yml", "invalid: [yaml\n")
	selected := writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)
	var notified []string
	session := checking.Options{WorkingDir: root, OnFilesSelected: func(paths []string) { notified = paths }, ReadFile: func(path string) ([]byte, error) {
		if path == excluded {
			t.Error("excluded workflow was read")
		}
		return os.ReadFile(path)
	}}
	var err error

	result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Repository, Directory: ""})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("excluded workflow affected analysis: %+v, %v", result, err)
	}
	if !slices.Contains(result.Inputs, excluded) || !slices.Contains(result.Inputs, selected) {
		t.Fatalf("discovered candidates missing from protected inputs: %v", result.Inputs)
	}
	if !slices.Equal(notified, []string{selected, excluded}) {
		t.Fatalf("discovery callback lost candidate paths: %v", notified)
	}
}

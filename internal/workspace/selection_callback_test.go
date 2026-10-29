package workspace_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestSelectionCallbackPreservesInputs(t *testing.T) {
	for _, excluded := range []string{"skip.yml", "*.yml"} {
		t.Run(excluded, func(t *testing.T) {
			root := t.TempDir()
			config := writeToolFixture(t, root, "actionlint.yml", "files: {excludes: ['"+excluded+"']}\n")
			paths := []string{
				writeToolFixture(t, root, "skip.yml", toolWorkflow),
				writeToolFixture(t, root, "keep.yml", toolWorkflow),
			}
			want := slices.Clone(paths)
			var selected []string
			calls := 0
			linter, err := workspace.New(t.Context(), workspace.Options{WorkingDir: root, ConfigFile: config, OnFilesSelected: func(files []string) {
				calls++
				selected = slices.Clone(files)
				if len(files) > 0 {
					files[0] = "changed.yml"
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := linter.Files(paths, nil); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !slices.Equal(selected, want) || !slices.Equal(paths, want) {
				t.Fatalf("callback calls=%d selected=%v inputs=%v, want %v", calls, selected, paths, want)
			}
		})
	}
}

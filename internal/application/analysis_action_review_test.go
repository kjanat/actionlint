package application_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
)

func TestActionDiscoverySymlinkRoot(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(strconv.FormatBool(mixed), func(t *testing.T) {
			root := t.TempDir()
			writeGitRepository(t, root)
			writeFixture(t, root, "action.yml", compositeAction)
			want := 1
			if mixed {
				writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)
				want++
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(root, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			session := application.Options{WorkingDir: alias}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Repository, Directory: ""})
			if err != nil || len(result.Documents) != want || len(result.Diagnostics) != 0 {
				t.Fatalf("want %d clean documents: result=%+v error=%v", want, result, err)
			}
		})
	}
}

func TestActionDiscoveryDoesNotHideWorkflowTypo(t *testing.T) {
	root := t.TempDir()
	writeGitRepository(t, root)
	writeFixture(t, root, "action.yml", compositeAction)
	writeFixture(t, root, ".github/workflow/ci.yml", goodWorkflow)
	session := application.Options{WorkingDir: root}
	var err error

	_, err = application.Check(t.Context(), session, application.Input{Kind: application.Repository, Directory: ""})
	if err == nil || !strings.Contains(err.Error(), "looks misspelled") {
		t.Fatalf("expected workflow typo despite action discovery, got %v", err)
	}
}

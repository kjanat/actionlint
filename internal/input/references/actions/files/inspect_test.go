package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	inputfiles "actionlint.kjanat.dev/v2/internal/input/files"
	actionfiles "actionlint.kjanat.dev/v2/internal/input/references/actions/files"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

func TestValidationUsesCapturedExecutionFileAvailability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	meta, err := actionmeta.Parse(filepath.Join(root, "action.yml"), []byte("name: test\ndescription: test\nruns: {using: node24, main: index.js, pre: absent.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "index.js")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	facts, err := actionfiles.Inspect(t.Context(), meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "absent.js"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := 0
	for _, finding := range actioncheck.Standalone(meta, facts) {
		if finding.Code == "missing-file" {
			missing++
			if !strings.Contains(finding.Message, `"absent.js"`) {
				t.Fatalf("validation used later filesystem state: %s", finding.Message)
			}
		}
	}
	if missing != 1 || facts.Lookup("index.js") != actionmeta.FilePresent || facts.Lookup("absent.js") != actionmeta.FileMissing {
		t.Fatalf("validation reread files: missing=%d", missing)
	}
	for _, finding := range actioncheck.Standalone(meta, actionmeta.ReferencedFiles{}) {
		if finding.Code == "missing-file" {
			t.Fatal("unknown availability became a missing-file finding")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := actionfiles.Inspect(ctx, meta, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestExecutionFileSharedByEntrypointsIsReadOnce(t *testing.T) {
	t.Parallel()
	meta := &actionmeta.Metadata{Directory: t.TempDir(), Runs: actionmeta.Runs{Using: "node24", Main: "shared.js", Pre: "shared.js", Post: "shared.js"}}
	reads := 0
	reader := inputfiles.New(t.Context(), func(string) ([]byte, error) { reads++; return nil, nil }, nil)
	facts, err := actionfiles.Inspect(t.Context(), meta, reader)
	if err != nil || reads != 1 || facts.Lookup("shared.js") != actionmeta.FilePresent {
		t.Fatalf("entrypoint acquisition repeated or lost: reads=%d error=%v", reads, err)
	}
}

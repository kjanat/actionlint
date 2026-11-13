package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/edits"
)

func TestFixInputThroughAnotherSpelling(t *testing.T) {
	t.Parallel()
	const before = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo \"${{ github.head_ref }}\"\n"
	const after = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo \"${GITHUB_HEAD_REF}\"\n"
	for _, spelling := range []string{"symlinked directory", "symlinked file"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			physical := filepath.Join(root, "physical")
			if err := os.Mkdir(physical, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(physical, "action.yml")
			if err := os.WriteFile(target, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "alias", "action.yml")
			link, destination := filepath.Join(root, "alias"), physical
			if spelling == "symlinked file" {
				path = filepath.Join(root, "action.yml")
				link, destination = path, target
			}
			if err := os.Symlink(destination, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			args := []string{"actionlint", "fix", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff", "--action-metadata=off", "--include-suggested", path}
			var out, log bytes.Buffer
			if status := (&Command{Stdout: &out, Stderr: &log}).MainContext(t.Context(), args); status != 0 {
				t.Fatalf("preview status=%d stderr=%s stdout=%s", status, &log, &out)
			}
			var plan edits.Plan
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
				t.Fatalf("preview: %v: %s", err, &out)
			}
			if len(plan.Changes) != 1 || plan.Changes[0].After != after {
				t.Fatalf("plan=%+v", plan)
			}
			out.Reset()
			log.Reset()
			if status := (&Command{Stdout: &out, Stderr: &log}).MainContext(t.Context(), append(args, "--apply", "--quiet")); status != 0 {
				t.Fatalf("apply status=%d stderr=%s stdout=%s", status, &log, &out)
			}
			written, err := os.ReadFile(target)
			if err != nil || string(written) != after {
				t.Fatalf("written=%q error=%v", written, err)
			}
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("fix replaced the symlink: %v %v", info, err)
			}
		})
	}
}

func TestFixTargetReaderRejectsOtherFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := filepath.Join(root, "input.yml")
	other := filepath.Join(root, "other.yml")
	for _, path := range []string{input, other} {
		if err := os.WriteFile(path, []byte("on: push\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias.yml")
	if err := os.Symlink(other, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reader := fixTargetReader(t.Context(), []string{input, root})
	if _, err := reader.ReadFile(input); err != nil {
		t.Fatalf("analysis input rejected: %v", err)
	}
	for _, path := range []string{other, alias, filepath.Join(root, "missing.yml")} {
		if _, err := reader.ReadFile(path); err == nil || !strings.Contains(err.Error(), "was not an analysis input") {
			t.Fatalf("%s: error=%v", path, err)
		}
	}
}

package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/edits"
)

func TestFixPreviewAndApplyRechecks(t *testing.T) {
	t.Parallel()
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck is not installed")
	}
	// Resolve version-manager shims before changing directory.
	probe := exec.CommandContext(t.Context(), shellcheck, "--version")
	if err := probe.Run(); err != nil {
		t.Skipf("ShellCheck unavailable: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yml")
	content := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          echo $HOME\npermissions: {}\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"fix", "--no-config", "--no-tool=ruff", "--tool=shellcheck=" + shellcheck, "--include-suggested", path}
	result := testRunCommand("", args...)
	if result.Status != 0 {
		t.Fatal(result)
	}
	var plan edits.Plan
	if err := json.Unmarshal([]byte(result.Stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || !strings.Contains(plan.Changes[0].After, `echo "$HOME"`) {
		t.Fatalf("no quoting fix: %+v", plan)
	}
	original, _ := os.ReadFile(path)
	if string(original) != content {
		t.Fatal("preview modified source")
	}
	result = testRunCommand("", append(args, "--apply", "--quiet")...)
	if result.Status != 0 {
		t.Fatalf("post-apply check failed: %+v", result)
	}
	written, _ := os.ReadFile(path)
	if string(written) != plan.Changes[0].After {
		t.Fatalf("unexpected edits: %s", written)
	}
}

func TestFixUnusedSuppressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, before, after string }{
		{"all unused", "      - run: echo ok # ignore[expression-reference, script-interpolation] -- obsolete\n", "      - run: echo ok\n"},
		{"partially used", "      - run: echo '::set-output name=x::y' # actionlint:ignore deprecated-commands, script-interpolation -- keep reason\n", "      - run: echo '::set-output name=x::y' # actionlint:ignore deprecated-commands -- keep reason\n"},
		{"next line", "      # ignore-next-line expression-reference\n      - run: echo ok\n", "      - run: echo ok\n"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				t.Parallel()
				const prefix = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n"
				before := strings.ReplaceAll(prefix+tc.before+"permissions: {}\n", "\n", ending)
				after := strings.ReplaceAll(prefix+tc.after+"permissions: {}\n", "\n", ending)
				path := filepath.Join(t.TempDir(), "ci.yml")
				if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
					t.Fatal(err)
				}
				got := testRunCommand("", "fix", "--norc", path)
				var plan edits.Plan
				if err := json.Unmarshal([]byte(got.Stdout), &plan); err != nil {
					t.Fatalf("preview: %+v: %v", got, err)
				}
				if got.Status != 0 || len(plan.Changes) != 1 || plan.Skipped != 0 || plan.Changes[0].After != after {
					t.Fatalf("unsafe or overlapping suppression cleanup: %+v, %+v", got, plan)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != before {
					t.Fatalf("preview modified source: %q, %v", data, err)
				}
				got = testRunCommand("", "fix", "--apply", "--norc", path)
				data, err = os.ReadFile(path)
				if got.Status != 0 || err != nil || string(data) != after {
					t.Fatalf("cleanup did not apply and recheck cleanly: %+v; bytes=%q, err=%v", got, data, err)
				}
			})
		}
	}
}

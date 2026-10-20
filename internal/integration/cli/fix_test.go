package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fix"
)

func TestFixPreviewAndApplyRechecks(t *testing.T) {
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
	content := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          echo $HOME\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"fix", "--no-config", "--no-ruff", "--shellcheck=" + shellcheck, "--include-suggested", path}
	result := testRunCommand("", args...)
	if result.Status != 0 {
		t.Fatal(result)
	}
	var plan fix.Plan
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

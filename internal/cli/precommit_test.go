package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPreCommitCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout []string
		status int
		stdout string
		stderr string
	}{
		{"no repository", nil, 0, "", ""},
		{"no workflows directory", []string{".git"}, 0, "", ""},
		{"empty workflows directory", []string{".git", ".github/workflows"}, 0, "", ""},
		{"misspelled workflows directory", []string{".git", ".gihub/workflows"}, 3, "", "looks misspelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, path := range tc.layout {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			got := testRunCommand("", "pre-commit")
			if got.Status != tc.status || got.Stdout != tc.stdout || !strings.Contains(got.Stderr, tc.stderr) || (tc.stderr == "" && got.Stderr != "") {
				t.Fatalf("got %+v, want status %d stdout %q stderr containing %q", got, tc.status, tc.stdout, tc.stderr)
			}
		})
	}
}

func TestPreCommitCommandChecksRepository(t *testing.T) {
	commandTestRepo(t)
	got := testRunCommand("", "pre-commit")
	if got.Status != 1 || !strings.Contains(got.Stdout, `undefined variable "missing"`) {
		t.Fatalf("expected the workflow finding, got %+v", got)
	}
	if got := testRunCommand("", "pre-commit", "--ignore-regex", "missing"); got.Status != 0 || got.Stdout != "" {
		t.Fatalf("check flags must apply, got %+v", got)
	}
	if got := testRunCommand("", "pre-commit", "ci.yml"); got.Status != 2 {
		t.Fatalf("positional arguments must be rejected, got %+v", got)
	}
}

func TestPreCommitCommandVerbose(t *testing.T) {
	t.Chdir(t.TempDir())
	got := testRunCommand("", "pre-commit", "--verbose")
	if got.Status != 0 || got.Stdout != "" || !strings.Contains(got.Stderr, "No workflow files to check") {
		t.Fatalf("got %+v", got)
	}
	if got := testRunCommand("", "pre-commit", "--verbose", "--quiet"); got.Stderr != "" {
		t.Fatalf("--quiet must silence the notice, got %+v", got)
	}
}

func TestPreCommitCommandIsHidden(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"check", "--help"}, {"help"}} {
		got := testRunCommand("", args...)
		if got.Status != 0 || strings.Contains(got.Stdout, "pre-commit") {
			t.Fatalf("%v listed the hidden command: %+v", args, got)
		}
	}
	var out, errout bytes.Buffer
	cmd := Command{Stdout: &out, Stderr: &errout}
	if code := cmd.Main([]string{"actionlint", "__complete", ""}); code != 0 {
		t.Fatalf("exit=%d, stderr=%s", code, &errout)
	}
	if !strings.Contains(out.String(), "check") || strings.Contains(out.String(), "pre-commit") {
		t.Fatalf("completion output: %s", &out)
	}
}

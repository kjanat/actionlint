package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

func TestPreCommitCommandIgnoresFileNamedPreCommit(t *testing.T) {
	t.Chdir(t.TempDir())
	gitRepository(t, ".")
	if err := os.WriteFile("pre-commit", []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := testRunCommand("", "pre-commit"); got.Status != 0 || got.Stdout != "" || got.Stderr != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestPreCommitCommandReportsUnreadableWorkflows(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced")
	}
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(".github/workflows", 0o700); err != nil {
		t.Fatal(err)
	}
	gitRepository(t, ".")
	if err := os.Chmod(".github/workflows", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(".github/workflows", 0o700) })
	if got := testRunCommand("", "pre-commit"); got.Status != 3 || got.Stdout != "" || !strings.Contains(got.Stderr, "permission denied") {
		t.Fatalf("got %+v", got)
	}
}

func TestPreCommitCommandRendersEmptyResult(t *testing.T) {
	t.Chdir(t.TempDir())
	got := testRunCommand("", "pre-commit", "--json")
	if got.Status != 0 || !json.Valid([]byte(got.Stdout)) || !strings.Contains(got.Stdout, `"documents":[]`) || got.Stderr != "" {
		t.Fatalf("got %+v", got)
	}
	if err := os.WriteFile("report.json", []byte("previous report"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand("", "pre-commit", "--json", "--summary", "--output-file", "report.json")
	data, err := os.ReadFile("report.json")
	if err != nil || got.Status != 0 || got.Stdout != "" || !json.Valid(data) || got.Stderr != "{\"summary\":{\"files\":0,\"findings\":0}}\n" {
		t.Fatalf("%+v %s (%v)", got, data, err)
	}
}

func TestPreCommitCommandProtectsRepositoryConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".github", "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitRepository(t, ".")
	config := filepath.Join(".github", "actionlint.yaml")
	if err := os.WriteFile(config, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "pre-commit", "--json", "--output-file", config)
	data, err := os.ReadFile(config)
	if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") || err != nil || string(data) != "{}\n" {
		t.Fatalf("the empty report replaced the configuration: %+v %s (%v)", got, data, err)
	}
	got = testRunCommand("", "pre-commit", "--json")
	if got.Status != 0 || !strings.Contains(got.Stdout, `"configurations":[{`) || !strings.Contains(got.Stdout, "actionlint.yaml") {
		t.Fatalf("the empty result lost the configuration report: %+v", got)
	}
}

func TestPreCommitCommandProtectsListedDirectories(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".github", "workflows", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitRepository(t, ".")
	got := testRunCommand("", "pre-commit", "--json", "--output-file", filepath.Join(".github", "workflows", "nested"))
	if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
		t.Fatalf("a listed workflows directory is not an input: %+v", got)
	}
}

func TestPreCommitCommandReportsExplicitConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	gitRepository(t, ".")
	if err := os.WriteFile("lint.yaml", []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "pre-commit", "--config", "lint.yaml", "--json")
	if got.Status != 0 || !strings.Contains(got.Stdout, `"configurations":[{`) || !strings.Contains(got.Stdout, "lint.yaml") || got.Stderr != "" {
		t.Fatalf("the explicit configuration is missing from the report: %+v", got)
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

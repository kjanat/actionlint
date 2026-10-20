package workspace_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestGitIndexLocationCompatibility(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git compatibility shim requires a Unix shell")
	}
	root, _ := executableFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	const shim = `#!/bin/sh
case " $* " in
  *" rev-parse "*)
    if [ "$ACTIONLINT_TEST_INDEX_MODE" = old ]; then
      for arg do
        if [ "$arg" = --path-format=absolute ]; then
          printf '%s\n' "$arg"
        fi
      done
      exec "$ACTIONLINT_TEST_REAL_GIT" -C "$ACTIONLINT_TEST_REPOSITORY" rev-parse --git-path index
    fi
    printf '%s' "$ACTIONLINT_TEST_INDEX_OUTPUT"
    exit 0
    ;;
esac
exec "$ACTIONLINT_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_TEST_REAL_GIT", realGit)
	t.Setenv("ACTIONLINT_TEST_REPOSITORY", root)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name, mode, output string
		invalid            bool
	}{
		{"pre-2.31 option echo", "old", "", false},
		{"relative", "output", ".git/index\n", false},
		{"absolute", "output", filepath.Join(root, ".git", "index") + "\r\n", false},
		{"empty", "output", "\n", true},
		{"multiple lines", "output", "--path-format=absolute\n.git/index\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ACTIONLINT_TEST_INDEX_MODE", tc.mode)
			t.Setenv("ACTIONLINT_TEST_INDEX_OUTPUT", tc.output)
			snapshot := (&workspace.IndexCache{}).Load(t.Context(), root)
			if tc.invalid {
				if snapshot.Err == nil || snapshot.Path != "" {
					t.Fatalf("invalid index location accepted: %q, %v", snapshot.Path, snapshot.Err)
				}
				return
			}
			want := filepath.Join(root, ".git", "index")
			if snapshot.Err != nil || snapshot.Path != want || snapshot.Modes["bad.sh"] != "100644" {
				t.Fatalf("index location = %q, %v; want %q with parsed modes", snapshot.Path, snapshot.Err, want)
			}
		})
	}
}

func TestRepositoryGitDisablesFSMonitor(t *testing.T) {
	root, configure := executableFixture(t)
	configure("config", "core.fsmonitor", "true")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	output, err := workspace.GitCommand(t.Context(), git, root, "config", "--type=bool", "--get", "core.fsmonitor").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "false" {
		t.Fatalf("index queries must disable repository fsmonitor: %q, %v", output, err)
	}
	snapshot := (&workspace.IndexCache{}).Load(t.Context(), root)
	if snapshot.Err != nil || snapshot.Modes["bad.sh"] != "100644" {
		t.Fatalf("index mode query failed: %v, %v", snapshot.Modes, snapshot.Err)
	}
}

func TestGitIndexSeparateDirectory(t *testing.T) {
	root := t.TempDir()
	metadata := filepath.Join(t.TempDir(), "metadata")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	if output, err := workspace.GitCommand(t.Context(), git, root, "init", "--quiet", "--separate-git-dir", metadata).CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v: %s", err, output)
	}
	writeIndexFixture(t, root, "file.sh", "echo hello\n")
	if output, err := workspace.GitCommand(t.Context(), git, root, "add", "file.sh").CombinedOutput(); err != nil {
		t.Fatalf("index file: %v: %s", err, output)
	}
	snapshot := (&workspace.IndexCache{}).Load(t.Context(), root)
	if snapshot.Err != nil {
		t.Fatal(snapshot.Err)
	}
	actual, err := os.Stat(snapshot.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(metadata, "index")
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("separate index location %q does not identify %q", snapshot.Path, want)
	}
}

func TestGitModesIntentToAdd(t *testing.T) {
	root, git := executableFixture(t)
	writeIndexFixture(t, root, "staged-empty.sh", "")
	git("add", "staged-empty.sh")
	for _, name := range []string{"intent with spaces.sh", "deleted-intent.sh"} {
		writeIndexFixture(t, root, name, "echo hello\n")
		git("add", "-N", name)
	}
	if err := os.Remove(filepath.Join(root, "deleted-intent.sh")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := (&workspace.IndexCache{}).Load(t.Context(), root)
	if snapshot.Err != nil {
		t.Fatal(snapshot.Err)
	}
	for _, name := range []string{"intent.sh", "intent with spaces.sh", "deleted-intent.sh"} {
		if mode := snapshot.Modes[name]; mode != "" {
			t.Errorf("intent-to-add %q retained mode %s", name, mode)
		}
	}
	for _, name := range []string{"bad.sh", "staged-empty.sh"} {
		if mode := snapshot.Modes[name]; mode != "100644" {
			t.Errorf("staged file %q lost mode: %q", name, mode)
		}
	}
	after, err := os.ReadFile(snapshot.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("index inspection modified the index")
	}
}

func TestGitModesIntentToAddAfterRemoval(t *testing.T) {
	root, git := executableFixture(t)
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	git("config", "core.hooksPath", t.TempDir())
	writeIndexFixture(t, root, "empty.sh", "")
	git("add", "empty.sh")
	git("commit", "--no-gpg-sign", "-m", "baseline")
	git("rm", "--cached", "bad.sh", "empty.sh")
	git("add", "-N", "bad.sh", "empty.sh")
	snapshot := (&workspace.IndexCache{}).Load(t.Context(), root)
	if snapshot.Err != nil {
		t.Fatal(snapshot.Err)
	}
	for _, name := range []string{"bad.sh", "empty.sh"} {
		if mode := snapshot.Modes[name]; mode != "" {
			t.Errorf("removed HEAD path %q re-added with intent retained mode %s", name, mode)
		}
	}
	if mode := snapshot.Modes["good.sh"]; mode != "100755" {
		t.Errorf("unchanged committed file lost mode: %q", mode)
	}
}

func TestIndexCacheRetainsOneSessionSnapshot(t *testing.T) {
	root, git := executableFixture(t)
	cache := &workspace.IndexCache{}
	first := cache.Load(t.Context(), root)
	if first.Err != nil {
		t.Fatal(first.Err)
	}
	writeIndexFixture(t, root, "later.sh", "echo later\n")
	git("add", "later.sh")
	again := cache.Load(t.Context(), root)
	if again != first || again.Modes["later.sh"] != "" {
		t.Fatal("session snapshot changed after repository mutation")
	}
	fresh := (&workspace.IndexCache{}).Load(t.Context(), root)
	if fresh.Err != nil || fresh.Modes["later.sh"] != "100644" {
		t.Fatalf("new session lost index update: %+v", fresh)
	}
}

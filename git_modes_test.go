package actionlint

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/gitrepo"
)

func repositoryGit(ctx context.Context, root string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	return command
}

func TestGitModesIgnoreHookEnvironment(t *testing.T) {
	root, _ := executableFixture(t)
	t.Setenv("GIT_DIR", filepath.Join(root, "missing"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(root, "missing", "index"))
	t.Setenv("GIT_WORK_TREE", filepath.Join(root, "missing"))
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil || snapshot.modes["bad.sh"] != "100644" || snapshot.modes["good.sh"] != "100755" {
		t.Fatalf("index mode query failed: %v, %v", snapshot.modes, snapshot.err)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(physical, ".git", "index"); snapshot.index != want {
		t.Fatalf("index location = %q, want %q", snapshot.index, want)
	}
}

func TestGitModesOutsideRepository(t *testing.T) {
	snapshot := (&gitModes{}).load(t.Context(), t.TempDir())
	if snapshot.err == nil || snapshot.modes != nil {
		t.Fatalf("no repository: %v, %v", snapshot.modes, snapshot.err)
	}
}

func TestGitModesBareRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	if output, err := repositoryGit(t.Context(), root, "init", "--quiet", "--bare").CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v: %s", err, output)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err == nil || !strings.Contains(snapshot.err.Error(), "without a work tree") {
		t.Fatalf("bare repository: %v, %v", snapshot.modes, snapshot.err)
	}
}

func TestGitIndexSeparateDirectory(t *testing.T) {
	root := t.TempDir()
	metadata := filepath.Join(t.TempDir(), "metadata")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	if output, err := repositoryGit(t.Context(), root, "init", "--quiet", "--separate-git-dir", metadata).CombinedOutput(); err != nil {
		t.Fatalf("initialize repository: %v: %s", err, output)
	}
	writeShellcheckFixture(t, root, "file.sh", "echo hello\n")
	if output, err := repositoryGit(t.Context(), root, "add", "file.sh").CombinedOutput(); err != nil {
		t.Fatalf("index file: %v: %s", err, output)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	actual, err := os.Stat(snapshot.index)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(metadata, "index")
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("separate index location %q does not identify %q", snapshot.index, want)
	}
}

func TestGitModesIntentToAdd(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "staged-empty.sh", "")
	git("add", "staged-empty.sh")
	for _, name := range []string{"intent with spaces.sh", "deleted-intent.sh"} {
		writeShellcheckFixture(t, root, name, "echo hello\n")
		git("add", "-N", name)
	}
	if err := os.Remove(filepath.Join(root, "deleted-intent.sh")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	for _, name := range []string{"intent.sh", "intent with spaces.sh", "deleted-intent.sh"} {
		if mode := snapshot.modes[name]; mode != "" {
			t.Errorf("intent-to-add %q retained mode %s", name, mode)
		}
	}
	for _, name := range []string{"bad.sh", "staged-empty.sh"} {
		if mode := snapshot.modes[name]; mode != "100644" {
			t.Errorf("staged file %q lost mode: %q", name, mode)
		}
	}
	after, err := os.ReadFile(snapshot.index)
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
	writeShellcheckFixture(t, root, "empty.sh", "")
	git("add", "empty.sh")
	git("commit", "--no-gpg-sign", "-m", "baseline")
	git("rm", "--cached", "bad.sh", "empty.sh")
	git("add", "-N", "bad.sh", "empty.sh")
	snapshot := (&gitModes{}).load(t.Context(), root)
	if snapshot.err != nil {
		t.Fatal(snapshot.err)
	}
	for _, name := range []string{"bad.sh", "empty.sh"} {
		if mode := snapshot.modes[name]; mode != "" {
			t.Errorf("removed HEAD path %q re-added with intent retained mode %s", name, mode)
		}
	}
	if mode := snapshot.modes["good.sh"]; mode != "100755" {
		t.Errorf("unchanged committed file lost mode: %q", mode)
	}
}

func TestGitModeMap(t *testing.T) {
	got := gitModeMap([]gitrepo.IndexEntry{
		{Path: "plain", Mode: 0o100644},
		{Path: "exec", Mode: 0o100755},
		{Path: "link", Mode: 0o120000},
		{Path: "sub", Mode: 0o160000},
		{Path: "conflict", Mode: 0o100644, Stage: 1},
		{Path: "conflict", Mode: 0o100755, Stage: 2},
		{Path: "intent", Mode: 0o100644, IntentToAdd: true},
	})
	want := map[string]string{"plain": "100644", "exec": "100755", "link": "120000", "sub": "160000", "conflict": "unmerged"}
	if len(got) != len(want) {
		t.Fatalf("gitModeMap = %v, want %v", got, want)
	}
	for name, mode := range want {
		if got[name] != mode {
			t.Errorf("%s = %q, want %q", name, got[name], mode)
		}
	}
}

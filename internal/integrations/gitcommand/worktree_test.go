package gitcommand_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"
)

func worktreeFixture(t *testing.T) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	command := func(args ...string) {
		t.Helper()
		args = append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
		if out, err := gitcommand.Command(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	return root, command
}

func writeWorktreeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadWorktreeBeforeFirstCommit(t *testing.T) {
	root, _ := worktreeFixture(t)
	writeWorktreeFile(t, root, ".github/workflows/ci.yml", "on: push\n")
	got, err := gitcommand.ReadWorktree(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "" {
		t.Fatalf("Head = %q, want empty before the first commit", got.Head)
	}
	if want := []string{".github/workflows/ci.yml"}; !slices.Equal(got.Changes, want) {
		t.Fatalf("Changes = %q, want %q", got.Changes, want)
	}
}

func TestReadWorktreeDetectsCleanAndDirtyTrees(t *testing.T) {
	root, git := worktreeFixture(t)
	writeWorktreeFile(t, root, ".github/workflows/ci.yml", "on: push\n")
	writeWorktreeFile(t, root, ".gitignore", "ignored.yml\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "initial")

	clean, err := gitcommand.ReadWorktree(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Head) != 40 {
		t.Fatalf("Head = %q, want a full commit ID", clean.Head)
	}
	if clean.Dirty() || len(clean.Changes) != 0 {
		t.Fatalf("clean tree reported changes %q", clean.Changes)
	}

	writeWorktreeFile(t, root, ".github/workflows/ci.yml", "on: pull_request\n")
	writeWorktreeFile(t, root, ".github/workflows/new.yml", "on: push\n")
	writeWorktreeFile(t, root, "ignored.yml", "on: push\n")
	dirty, err := gitcommand.ReadWorktree(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if dirty.Head != clean.Head {
		t.Fatalf("Head changed from %q to %q without a commit", clean.Head, dirty.Head)
	}
	want := []string{".github/workflows/ci.yml", ".github/workflows/new.yml"}
	if !dirty.Dirty() || !slices.Equal(dirty.Changes, want) {
		t.Fatalf("Changes = %q, want %q", dirty.Changes, want)
	}
}

func TestParseStatusPathsSkipsRenameSources(t *testing.T) {
	got, err := gitcommand.ParseStatusPaths("R  new.yml\x00old.yml\x00 M ci.yml\x00?? space dir/a.yml\x00")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"new.yml", "ci.yml", "space dir/a.yml"}; !slices.Equal(got, want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if _, err := gitcommand.ParseStatusPaths("bad\x00"); err == nil {
		t.Fatal("malformed status entry was accepted")
	}
}

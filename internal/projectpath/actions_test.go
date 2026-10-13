package projectpath

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func gitFixture(t *testing.T, root string) func(...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	config := t.TempDir()
	writeFixture(t, config, "excludes", "global/\n")
	writeFixture(t, config, "gitconfig", "[user]\n\tname = Fixture\n\temail = fixture@example.invalid\n[commit]\n\tgpgsign = false\n[core]\n\texcludesFile = "+filepath.ToSlash(filepath.Join(config, "excludes"))+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(config, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "--quiet")
	return git
}

func populate(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"untracked/action.yaml", "ignored/action.yml", "excluded/action.yml", "global/action.yml"} {
		writeFixture(t, root, name, "name: fixture\n")
	}
}

func TestActionsHonourGitIgnore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string, git func(...string)) string
	}{
		{"ignore rules", func(t *testing.T, root string, git func(...string)) string { return root }},
		{"hook environment", func(t *testing.T, root string, git func(...string)) string {
			t.Setenv("GIT_DIR", filepath.Join(root, "missing"))
			t.Setenv("GIT_WORK_TREE", filepath.Join(root, "missing"))
			t.Setenv("GIT_INDEX_FILE", filepath.Join(root, "missing", "index"))
			return root
		}},
		{"git unavailable", func(t *testing.T, root string, git func(...string)) string {
			t.Setenv("PATH", t.TempDir())
			return root
		}},
		{"deleted tracked manifest", func(t *testing.T, root string, git func(...string)) string {
			writeFixture(t, root, "deleted/action.yml", "name: fixture\n")
			git("add", "--", "deleted/action.yml")
			git("commit", "--quiet", "-m", "Delete")
			if err := os.Remove(filepath.Join(root, "deleted", "action.yml")); err != nil {
				t.Fatal(err)
			}
			return root
		}},
		{"tracked symlink", func(t *testing.T, root string, git func(...string)) string {
			if err := os.MkdirAll(filepath.Join(root, "link"), 0750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("..", "tracked", "action.yml"), filepath.Join(root, "link", "action.yml")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			git("add", "--", "link/action.yml")
			return root
		}},
		{"symlinked directory", func(t *testing.T, root string, git func(...string)) string {
			writeFixture(t, root, "moved/action.yml", "name: fixture\n")
			git("add", "--", "moved/action.yml")
			git("commit", "--quiet", "-m", "Move")
			if err := os.RemoveAll(filepath.Join(root, "moved")); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			writeFixture(t, outside, "action.yml", "name: fixture\n")
			if err := os.Symlink(outside, filepath.Join(root, "moved")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return root
		}},
		{"nested repository", func(t *testing.T, root string, git func(...string)) string {
			writeFixture(t, root, "nested/action.yml", "name: fixture\n")
			git("add", "--", "nested/action.yml")
			writeFixture(t, root, "nested/.git", "gitdir: elsewhere\n")
			return root
		}},
		{"symlinked root", func(t *testing.T, root string, git func(...string)) string {
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(root, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return alias
		}},
		{"worktree", func(t *testing.T, root string, git func(...string)) string {
			worktree := filepath.Join(t.TempDir(), "worktree")
			git("worktree", "add", "--quiet", worktree)
			if info, err := os.Lstat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
				t.Fatalf("worktree .git must be a file: %v", err)
			}
			populate(t, worktree)
			return worktree
		}},
		{"index version 4", func(t *testing.T, root string, git func(...string)) string {
			git("update-index", "--index-version", "4")
			return root
		}},
		{"ignore case", func(t *testing.T, root string, git func(...string)) string {
			git("config", "core.ignoreCase", "true")
			writeFixture(t, root, ".gitignore", "IGNORED/\n")
			return root
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			git := gitFixture(t, root)
			for _, name := range []string{"tracked/action.yml", "node_modules/pkg/action.yml", ".github/workflows/action.yml", "README.md"} {
				writeFixture(t, root, name, "name: fixture\n")
			}
			writeFixture(t, root, ".gitignore", "ignored/\n")
			git("add", "--", ".gitignore", "tracked/action.yml", "node_modules/pkg/action.yml", ".github/workflows/action.yml", "README.md")
			git("commit", "--quiet", "-m", "Fixture")
			writeFixture(t, root, ".git/info/exclude", "excluded/\n")
			populate(t, root)
			discovery := tc.setup(t, root, git)
			got, err := Actions(t.Context(), discovery)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{filepath.Join(discovery, "tracked", "action.yml"), filepath.Join(discovery, "untracked", "action.yaml")}
			if !slices.Equal(got, want) {
				t.Fatalf("Actions = %q, want %q", got, want)
			}
		})
	}
}

func TestActionsTrackedInIgnoredDirectory(t *testing.T) {
	root := t.TempDir()
	git := gitFixture(t, root)
	writeFixture(t, root, ".gitignore", "build/\n")
	writeFixture(t, root, "build/kept/action.yml", "name: fixture\n")
	writeFixture(t, root, "build/other/action.yml", "name: fixture\n")
	git("add", "--force", "--", ".gitignore", "build/kept/action.yml")
	got, err := Actions(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "build", "kept", "action.yml")}; !slices.Equal(got, want) {
		t.Fatalf("Actions = %q, want %q", got, want)
	}
}

func TestActionsInsideSubdirectory(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root)
	writeFixture(t, root, ".gitignore", "ignored/\n")
	writeFixture(t, root, "packages/.gitignore", "local/\n")
	writeFixture(t, root, "packages/app/action.yml", "name: fixture\n")
	writeFixture(t, root, "packages/ignored/action.yml", "name: fixture\n")
	writeFixture(t, root, "packages/local/action.yml", "name: fixture\n")
	start := filepath.Join(root, "packages")
	got, err := Actions(t.Context(), start)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(start, "app", "action.yml")}; !slices.Equal(got, want) {
		t.Fatalf("Actions = %q, want %q", got, want)
	}
}

func TestActionsDirectoryError(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "walk", true: "git"}[repository], func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "loop/action.yml", "name: fixture\n")
			if repository {
				git := gitFixture(t, root)
				git("add", "--", "loop/action.yml")
			}
			if err := os.Symlink(".git", filepath.Join(root, "loop", ".git")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if got, err := Actions(t.Context(), root); err == nil {
				t.Fatalf("Actions = %q, want an error for the looping .git link", got)
			}
		})
	}
}

func seal(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0750); err != nil {
			t.Fatal(err)
		}
	})
}

func TestActionsRepositoryError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string, git func(...string))
	}{
		{"unreadable tracked directory", func(t *testing.T, root string, git func(...string)) {
			writeFixture(t, root, "sealed/action.yml", "name: fixture\n")
			git("add", "--", "sealed/action.yml")
			seal(t, filepath.Join(root, "sealed"))
		}},
		{"unreadable untracked directory", func(t *testing.T, root string, git func(...string)) {
			writeFixture(t, root, "sealed/action.yml", "name: fixture\n")
			seal(t, filepath.Join(root, "sealed"))
		}},
		{"unreadable ignore file", func(t *testing.T, root string, git func(...string)) {
			writeFixture(t, root, "sub/.gitignore", "x\n")
			writeFixture(t, root, "sub/action.yml", "name: fixture\n")
			seal(t, filepath.Join(root, "sub", ".gitignore"))
		}},
		{"split index", func(t *testing.T, root string, git func(...string)) {
			writeFixture(t, root, "action.yml", "name: fixture\n")
			git("add", "--", "action.yml")
			git("update-index", "--split-index")
		}},
		{"broken repository", func(t *testing.T, root string, git func(...string)) {
			writeFixture(t, root, "action.yml", "name: fixture\n")
			if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, ".git", "gitdir: elsewhere\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			git := gitFixture(t, root)
			tc.setup(t, root, git)
			if got, err := Actions(t.Context(), root); err == nil {
				t.Fatalf("Actions = %q, want an error", got)
			}
		})
	}
}

func TestActionsCancelled(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "action.yml", "name: fixture\n")
	gitFixture(t, root)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := Actions(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("Actions = %q, %v, want %v", got, err, context.Canceled)
	}
}

func TestActionsOutsideRepository(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"action.yml", "ignored/action.yml", "node_modules/action.yml"} {
		writeFixture(t, root, name, "name: fixture\n")
	}
	writeFixture(t, root, ".gitignore", "ignored/\n")
	got, err := Actions(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "action.yml"), filepath.Join(root, "ignored", "action.yml")}
	if !slices.Equal(got, want) {
		t.Fatalf("Actions = %q, want %q", got, want)
	}
}

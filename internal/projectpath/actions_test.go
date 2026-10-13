package projectpath

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestActionsWalkFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"no repository", func(t *testing.T, root string) {}},
		{"invalid repository", func(t *testing.T, root string) {
			writeFixture(t, root, ".git", "gitdir: elsewhere\n")
		}},
		{"git unavailable", func(t *testing.T, root string) {
			gitFixture(t, root)
			t.Setenv("PATH", t.TempDir())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"action.yml", "ignored/action.yml", "node_modules/action.yml"} {
				writeFixture(t, root, name, "name: fixture\n")
			}
			writeFixture(t, root, ".gitignore", "ignored/\n")
			tc.setup(t, root)
			got, err := Actions(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{filepath.Join(root, "action.yml"), filepath.Join(root, "ignored", "action.yml")}
			if !slices.Equal(got, want) {
				t.Fatalf("Actions = %q, want %q", got, want)
			}
		})
	}
}

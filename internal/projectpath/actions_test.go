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

	"actionlint.kjanat.dev/internal/fsread"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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

func actions(t *testing.T, ctx context.Context, root string) ([]string, error) {
	t.Helper()
	return Actions(ctx, root, fsread.New(ctx, nil, nil))
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
		{"inherited pathspec and trace settings", func(t *testing.T, root string, git func(...string)) string {
			for _, name := range []string{"GIT_LITERAL_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_TRACE", "GIT_TRACE2", "GIT_TRACE_PERFORMANCE", "GIT_TRACE_SETUP"} {
				t.Setenv(name, "1")
			}
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
			if err := os.MkdirAll(filepath.Join(root, "link"), 0o750); err != nil {
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
		{"subdirectory", func(t *testing.T, root string, git func(...string)) string {
			return filepath.Join(root, "sub")
		}},
		{"worktree", func(t *testing.T, root string, git func(...string)) string {
			worktree := filepath.Join(t.TempDir(), "worktree")
			git("worktree", "add", "--quiet", worktree)
			if info, err := os.Lstat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
				t.Fatalf("worktree .git must be a file: %v", err)
			}
			populate(t, worktree)
			populate(t, filepath.Join(worktree, "sub"))
			return worktree
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			git := gitFixture(t, root)
			for _, name := range []string{"tracked/action.yml", "ignored/kept/action.yml", "sub/tracked/action.yml", "node_modules/pkg/action.yml", ".github/workflows/action.yml", "README.md"} {
				writeFixture(t, root, name, "name: fixture\n")
			}
			writeFixture(t, root, ".gitignore", "ignored/\n")
			git("add", "--", ".gitignore", "tracked/action.yml", "sub/tracked/action.yml", "node_modules/pkg/action.yml", ".github/workflows/action.yml", "README.md")
			git("add", "--force", "--", "ignored/kept/action.yml")
			git("commit", "--quiet", "-m", "Fixture")
			writeFixture(t, root, ".git/info/exclude", "excluded/\n")
			populate(t, root)
			populate(t, filepath.Join(root, "sub"))
			discovery := tc.setup(t, root, git)
			got, err := actions(t, t.Context(), discovery)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{"ignored/kept/action.yml", "sub/tracked/action.yml", "sub/untracked/action.yaml", "tracked/action.yml", "untracked/action.yaml"}
			if filepath.Base(discovery) == "sub" {
				names = []string{"tracked/action.yml", "untracked/action.yaml"}
			}
			want := make([]string, 0, len(names))
			for _, name := range names {
				want = append(want, filepath.Join(discovery, filepath.FromSlash(name)))
			}
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
			if got, err := actions(t, t.Context(), root); err == nil {
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
		if err := os.Chmod(path, 0o750); err != nil {
			t.Fatal(err)
		}
	})
}

func TestActionsGitError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		{"invalid repository", func(t *testing.T, root string) {
			writeFixture(t, root, ".git", "gitdir: elsewhere\n")
		}},
		{"git unavailable", func(t *testing.T, root string) {
			gitFixture(t, root)
			t.Setenv("PATH", t.TempDir())
		}},
		{"unreadable ignore file", func(t *testing.T, root string) {
			gitFixture(t, root)
			seal(t, filepath.Join(root, ".gitignore"))
		}},
		{"unreadable tracked directory", func(t *testing.T, root string) {
			git := gitFixture(t, root)
			writeFixture(t, root, "sealed/action.yml", "name: fixture\n")
			git("add", "--", "sealed/action.yml")
			seal(t, filepath.Join(root, "sealed"))
		}},
		{"unreadable untracked directory", func(t *testing.T, root string) {
			gitFixture(t, root)
			writeFixture(t, root, "sealed/action.yml", "name: fixture\n")
			seal(t, filepath.Join(root, "sealed"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"action.yml", "ignored/action.yml"} {
				writeFixture(t, root, name, "name: fixture\n")
			}
			writeFixture(t, root, ".gitignore", "ignored/\n")
			tc.setup(t, root)
			if got, err := actions(t, t.Context(), root); err == nil {
				t.Fatalf("Actions = %q, want an error", got)
			}
		})
	}
}

func TestActionsOutsideRepository(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"action.yml", "ignored/action.yml", "node_modules/action.yml"} {
		writeFixture(t, root, name, "name: fixture\n")
	}
	writeFixture(t, root, ".gitignore", "ignored/\n")
	got, err := actions(t, t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "action.yml"), filepath.Join(root, "ignored", "action.yml")}; !slices.Equal(got, want) {
		t.Fatalf("Actions = %q, want %q", got, want)
	}
}

func TestActionsReportReadDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "a/b/action.yml", "name: fixture\n")
	writeFixture(t, root, "node_modules/x/action.yml", "name: fixture\n")
	writeFixture(t, root, "nested/.git", "gitdir: elsewhere\n")
	writeFixture(t, root, ".github/workflows/action.yml", "on: push\n")
	var got []string
	paths, err := Actions(t.Context(), root, fsread.New(t.Context(), nil, func(path string) { got = append(got, path) }))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "a", "b", "action.yml")}; !slices.Equal(paths, want) {
		t.Fatalf("Actions = %q, want %q", paths, want)
	}
	slices.Sort(got)
	want := []string{root, filepath.Join(root, ".github"), filepath.Join(root, "a"), filepath.Join(root, "a", "b")}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("reported %q, want %q", got, want)
	}
}

func TestActionsReportGitInputs(t *testing.T) {
	root := t.TempDir()
	git := gitFixture(t, root)
	writeFixture(t, root, ".gitignore", "ignored/\n")
	writeFixture(t, root, "sub/.gitignore", "local/\n")
	writeFixture(t, root, "sub/deep/action.yml", "name: fixture\n")
	writeFixture(t, root, "ignored/deep/action.yml", "name: fixture\n")
	writeFixture(t, root, "sub/local/action.yml", "name: fixture\n")
	writeFixture(t, root, ".git/info/exclude", "excluded/\n")
	git("add", "--", ".gitignore")
	config := filepath.Dir(os.Getenv("GIT_CONFIG_GLOBAL"))
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		start string
		want  []string
	}{
		{root, []string{root, filepath.Join(root, ".gitignore"), filepath.Join(root, "sub"), filepath.Join(root, "sub", ".gitignore"), filepath.Join(root, "sub", "deep")}},
		{filepath.Join(root, "sub"), []string{filepath.Join(root, "sub"), filepath.Join(physical, ".gitignore"), filepath.Join(root, "sub", ".gitignore"), filepath.Join(root, "sub", "deep")}},
	} {
		got := map[string]bool{}
		paths, err := Actions(t.Context(), tc.start, fsread.New(t.Context(), nil, func(path string) { got[path] = true }))
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{filepath.Join(root, "sub", "deep", "action.yml")}; !slices.Equal(paths, want) {
			t.Fatalf("Actions(%s) = %q, want %q", tc.start, paths, want)
		}
		for _, path := range slices.Concat(tc.want, []string{filepath.Join(config, "excludes"), filepath.Join(physical, ".git", "info", "exclude"), filepath.Join(physical, ".git", "index")}) {
			if !got[path] {
				t.Errorf("Actions(%s) did not report %s in %v", tc.start, path, got)
			}
		}
		for _, path := range []string{filepath.Join(root, "ignored"), filepath.Join(root, "ignored", "deep"), filepath.Join(root, "sub", "local"), filepath.Join(root, ".git")} {
			if got[path] {
				t.Errorf("Actions(%s) reported the unread directory %s", tc.start, path)
			}
		}
	}
}

func TestActionsReportLogicalDirectories(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "a/action.yml", "name: fixture\n")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var got []string
	paths, err := Actions(t.Context(), alias, fsread.New(t.Context(), nil, func(path string) { got = append(got, path) }))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(alias, "a", "action.yml")}; !slices.Equal(paths, want) {
		t.Fatalf("Actions = %q, want %q", paths, want)
	}
	slices.Sort(got)
	if want := []string{alias, filepath.Join(alias, "a")}; !slices.Equal(got, want) {
		t.Fatalf("reported %q, want %q", got, want)
	}
}

func TestActionsCancelled(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "walk", true: "git"}[repository], func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "action.yml", "name: fixture\n")
			if repository {
				gitFixture(t, root)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if got, err := actions(t, ctx, root); !errors.Is(err, context.Canceled) {
				t.Fatalf("Actions = %q, %v, want context.Canceled", got, err)
			}
		})
	}
}

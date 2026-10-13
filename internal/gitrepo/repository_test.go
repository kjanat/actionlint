package gitrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenOutsideRepository(t *testing.T) {
	repo, err := Open(t.TempDir())
	if err != nil || repo != nil {
		t.Fatalf("Open = %v, %v, want nil, nil", repo, err)
	}
}

func TestOpenFindsAncestor(t *testing.T) {
	fixture := newTestRepo(t)
	nested := fixture.write("a/b/file", "")
	repo, err := Open(filepath.Dir(nested))
	if err != nil {
		t.Fatal(err)
	}
	if repo.WorkTree != fixture.root || repo.GitDir != filepath.Join(fixture.root, ".git") || repo.CommonDir != repo.GitDir {
		t.Fatalf("Open = %+v", repo)
	}
}

func TestOpenWorktree(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	fixture.git("commit", "--quiet", "-m", "Fixture")
	worktree := filepath.Join(t.TempDir(), "wt")
	fixture.git("worktree", "add", "--quiet", worktree)
	repo, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if repo.WorkTree != worktree {
		t.Fatalf("WorkTree = %q, want %q", repo.WorkTree, worktree)
	}
	mainGitDir, err := filepath.EvalSymlinks(filepath.Join(fixture.root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(repo.GitDir, filepath.Join(mainGitDir, "worktrees")) {
		t.Fatalf("GitDir = %q", repo.GitDir)
	}
	if repo.CommonDir != mainGitDir {
		t.Fatalf("CommonDir = %q, want %q", repo.CommonDir, mainGitDir)
	}
}

func TestOpenRejectsBrokenGitFile(t *testing.T) {
	for name, content := range map[string]string{"no gitdir line": "nonsense\n", "missing target": "gitdir: elsewhere\n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ".git"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if repo, err := Open(root); err == nil {
				t.Fatalf("Open = %+v, want an error", repo)
			}
		})
	}
}

func TestOpenReadsConfig(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "core.ignoreCase", "true")
	fixture.git("config", "core.excludesFile", "~/global-ignore")
	repo := fixture.open()
	if !repo.ignoreCase {
		t.Fatal("ignoreCase = false")
	}
	if want := filepath.Join(fixture.home, "global-ignore"); repo.excludes != want {
		t.Fatalf("excludes = %q, want %q", repo.excludes, want)
	}
	if repo.hashLength != 20 {
		t.Fatalf("hashLength = %d", repo.hashLength)
	}
}

func TestOpenExcludesFileSettings(t *testing.T) {
	for name, tc := range map[string]struct{ value, want string }{
		"empty disables": {"", ""},
		"relative":       {"mine", "mine"},
		"nested":         {"config/ignore", filepath.Join("config", "ignore")},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.git("config", "core.excludesFile", tc.value)
			want := tc.want
			if want != "" {
				want = filepath.Join(fixture.root, want)
			}
			if repo := fixture.open(); repo.excludes != want {
				t.Fatalf("excludes = %q, want %q", repo.excludes, want)
			}
		})
	}
}

func TestOpenAbsoluteCommonDir(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	fixture.git("commit", "--quiet", "-m", "Fixture")
	worktree := filepath.Join(t.TempDir(), "wt")
	fixture.git("worktree", "add", "--quiet", worktree)
	linked, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked.GitDir, "commondir"), []byte(linked.CommonDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := Open(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if again.CommonDir != linked.CommonDir {
		t.Fatalf("CommonDir = %q, want %q", again.CommonDir, linked.CommonDir)
	}
}

func TestOpenSymlinkedGitDir(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	elsewhere := filepath.Join(t.TempDir(), "metadata")
	if err := os.Rename(filepath.Join(fixture.root, ".git"), elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(fixture.root, ".git")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fixture.git("status", "--short")
	repo := fixture.open()
	if repo.GitDir != filepath.Join(fixture.root, ".git") {
		t.Fatalf("GitDir = %q", repo.GitDir)
	}
	if got, err := repo.TrackedFiles(); err != nil || len(got) != 1 {
		t.Fatalf("TrackedFiles = %q, %v", got, err)
	}
}

func TestOpenDefaultExcludesFile(t *testing.T) {
	fixture := newTestRepo(t)
	repo := fixture.open()
	if want := filepath.Join(fixture.home, ".config", "git", "ignore"); repo.excludes != want {
		t.Fatalf("excludes = %q, want %q", repo.excludes, want)
	}
}

func TestOpenGlobalConfigFile(t *testing.T) {
	fixture := newTestRepo(t)
	global := filepath.Join(fixture.home, "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\n\texcludesFile = /elsewhere/ignore\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if repo := fixture.open(); repo.excludes != filepath.FromSlash("/elsewhere/ignore") {
		t.Fatalf("excludes = %q", repo.excludes)
	}
}

func TestOpenWithoutHome(t *testing.T) {
	for name, home := range map[string]string{"unset": "", "dev null": os.DevNull} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write(".gitignore", "ignored/\n")
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if home == "" {
				for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
					if err := os.Unsetenv(name); err != nil {
						t.Fatal(err)
					}
				}
			}
			repo := fixture.open()
			ignores, err := repo.Ignores()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := ignores.Ignored("ignored", true); err != nil || !got {
				t.Fatalf("Ignored(ignored) = %v, %v", got, err)
			}
			if _, err := repo.TrackedFiles(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenSha256(t *testing.T) {
	fixture := newTestRepo(t, "--object-format=sha256")
	if repo := fixture.open(); repo.hashLength != 32 {
		t.Fatalf("hashLength = %d, want 32", repo.hashLength)
	}
}

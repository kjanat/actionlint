package gitrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestOpenOutsideRepository(t *testing.T) {
	repo, err := Open(t.Context(), t.TempDir())
	if err != nil || repo != nil {
		t.Fatalf("Open = %v, %v, want nil, nil", repo, err)
	}
}

type gitOutcome int

const (
	gitFound gitOutcome = iota
	gitAbsent
	gitFailed
)

type gitLocation struct {
	workTree, gitDir, commonDir string
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func gitIn(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	var stderr strings.Builder
	command.Stderr = &stderr
	out, err := command.Output()
	return strings.TrimRight(string(out), "\r\n"), stderr.String(), err
}

func gitDiscovery(t *testing.T, dir string) (gitLocation, gitOutcome, string) {
	t.Helper()
	gitDir, stderr, err := gitIn(t, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		if strings.Contains(stderr, "not a git repository (or any") {
			return gitLocation{}, gitAbsent, stderr
		}
		return gitLocation{}, gitFailed, stderr
	}
	common, stderr, err := gitIn(t, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatalf("git rev-parse --git-common-dir: %v: %s", err, stderr)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(resolvedPath(t, dir), common)
	}
	location := gitLocation{gitDir: resolvedPath(t, gitDir), commonDir: resolvedPath(t, common)}
	if top, _, err := gitIn(t, dir, "rev-parse", "--show-toplevel"); err == nil {
		location.workTree = resolvedPath(t, top)
	}
	return location, gitFound, stderr
}

func requireMatchesGit(t *testing.T, dir string) *Repository {
	t.Helper()
	want, outcome, stderr := gitDiscovery(t, dir)
	repo, err := Open(t.Context(), dir)
	switch outcome {
	case gitAbsent:
		if err != nil || repo != nil {
			t.Fatalf("Open(%q) = %+v, %v; git: %s", dir, repo, err, stderr)
		}
		return nil
	case gitFailed:
		if err == nil {
			t.Fatalf("Open(%q) = %+v, want an error; git: %s", dir, repo, stderr)
		}
		return nil
	}
	if err != nil || repo == nil {
		t.Fatalf("Open(%q) = %+v, %v; git finds %+v", dir, repo, err, want)
	}
	got := gitLocation{gitDir: resolvedPath(t, repo.GitDir), commonDir: resolvedPath(t, repo.CommonDir)}
	if repo.WorkTree != "" {
		got.workTree = resolvedPath(t, repo.WorkTree)
	}
	if got != want {
		t.Fatalf("Open(%q) = %+v, git finds %+v", dir, got, want)
	}
	return repo
}

func (r *testRepo) commit() {
	r.t.Helper()
	r.write("file", "")
	r.git("add", "file")
	r.git("commit", "--quiet", "-m", "Fixture")
}

func (r *testRepo) worktree(name string) string {
	r.t.Helper()
	r.commit()
	worktree := filepath.Join(filepath.Dir(r.root), name)
	r.git("worktree", "add", "--quiet", worktree)
	return worktree
}

func (r *testRepo) mkdir(name string) string {
	r.t.Helper()
	path := filepath.Join(filepath.Dir(r.root), filepath.FromSlash(name))
	if err := os.MkdirAll(path, 0750); err != nil {
		r.t.Fatal(err)
	}
	return path
}

func (r *testRepo) gitFile(name, content string) string {
	r.t.Helper()
	dir := r.mkdir(name)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte(content), 0600); err != nil {
		r.t.Fatal(err)
	}
	return dir
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestOpenMatchesGit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r *testRepo) string
	}{
		{"top level", func(t *testing.T, r *testRepo) string { return r.root }},
		{"subdirectory", func(t *testing.T, r *testRepo) string { return filepath.Dir(r.write("a/b/file", "")) }},
		{"inside the git directory", func(t *testing.T, r *testRepo) string { return filepath.Join(r.root, ".git", "refs", "heads") }},
		{"bare repository", func(t *testing.T, r *testRepo) string {
			bare := r.mkdir("bare.git")
			if _, stderr, err := gitIn(t, bare, "init", "--quiet", "--bare"); err != nil {
				t.Fatalf("git init --bare: %v: %s", err, stderr)
			}
			return bare
		}},
		{"inside a bare repository", func(t *testing.T, r *testRepo) string {
			bare := r.mkdir("bare.git")
			if _, stderr, err := gitIn(t, bare, "init", "--quiet", "--bare"); err != nil {
				t.Fatalf("git init --bare: %v: %s", err, stderr)
			}
			return filepath.Join(bare, "refs")
		}},
		{"core.bare", func(t *testing.T, r *testRepo) string {
			r.git("config", "core.bare", "true")
			return r.root
		}},
		{"relative core.worktree", func(t *testing.T, r *testRepo) string {
			r.mkdir("repo/wt")
			r.git("config", "core.worktree", "../wt")
			return filepath.Join(r.root, "wt")
		}},
		{"relative core.worktree outside the start", func(t *testing.T, r *testRepo) string {
			r.mkdir("elsewhere")
			r.git("config", "core.worktree", "../../elsewhere")
			return r.root
		}},
		{"absolute core.worktree", func(t *testing.T, r *testRepo) string {
			r.git("config", "core.worktree", r.mkdir("elsewhere"))
			return r.root
		}},
		{"core.bare beats core.worktree", func(t *testing.T, r *testRepo) string {
			r.mkdir("repo/wt")
			r.git("config", "core.worktree", "../wt")
			r.git("config", "core.bare", "true")
			return r.root
		}},
		{"linked worktree", func(t *testing.T, r *testRepo) string { return r.worktree("wt") }},
		{"linked worktree subdirectory", func(t *testing.T, r *testRepo) string {
			r.worktree("wt")
			return r.mkdir("wt/a/b")
		}},
		{"linked worktree ignores the common core.bare", func(t *testing.T, r *testRepo) string {
			worktree := r.worktree("wt")
			r.git("config", "core.bare", "true")
			return worktree
		}},
		{"worktreeConfig keeps the common core.bare", func(t *testing.T, r *testRepo) string {
			worktree := r.worktree("wt")
			r.git("config", "extensions.worktreeConfig", "true")
			r.git("config", "core.bare", "true")
			return worktree
		}},
		{"config.worktree core.bare", func(t *testing.T, r *testRepo) string {
			worktree := r.worktree("wt")
			r.git("config", "extensions.worktreeConfig", "true")
			if _, stderr, err := gitIn(t, worktree, "config", "--worktree", "core.bare", "true"); err != nil {
				t.Fatalf("git config --worktree: %v: %s", err, stderr)
			}
			return worktree
		}},
		{"separate git directory", func(t *testing.T, r *testRepo) string {
			work := r.mkdir("work")
			if _, stderr, err := gitIn(t, work, "init", "--quiet", "--separate-git-dir", filepath.Join(filepath.Dir(r.root), "metadata")); err != nil {
				t.Fatalf("git init --separate-git-dir: %v: %s", err, stderr)
			}
			return work
		}},
		{"relative gitfile with dot-dot", func(t *testing.T, r *testRepo) string {
			return r.gitFile("other", "gitdir: ../repo/.git\n")
		}},
		{"gitfile with CRLF and blank lines", func(t *testing.T, r *testRepo) string {
			return r.gitFile("other", "gitdir: ../repo/.git\r\n\r\n")
		}},
		{"gitfile without newline", func(t *testing.T, r *testRepo) string {
			return r.gitFile("other", "gitdir: ../repo/.git")
		}},
		{"absolute gitfile", func(t *testing.T, r *testRepo) string {
			return r.gitFile("other", "gitdir: "+filepath.ToSlash(filepath.Join(r.root, ".git"))+"\n")
		}},
		{"gitfile resolves dot-dot after symlinks", func(t *testing.T, r *testRepo) string {
			deep := r.mkdir("deep/x")
			symlink(t, deep, filepath.Join(filepath.Dir(r.root), "link"))
			return r.gitFile("other", "gitdir: ../link/../../repo/.git\n")
		}},
		{"gitfile naming a linked worktree git directory", func(t *testing.T, r *testRepo) string {
			linked, err := Open(t.Context(), r.worktree("wt"))
			if err != nil {
				t.Fatal(err)
			}
			return r.gitFile("alias", "gitdir: "+filepath.ToSlash(linked.GitDir)+"\n")
		}},
		{"absolute commondir", func(t *testing.T, r *testRepo) string {
			worktree := r.worktree("wt")
			linked, err := Open(t.Context(), worktree)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(linked.GitDir, "commondir"), []byte(filepath.Join(r.root, ".git")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			return worktree
		}},
		{"symlinked .git directory", func(t *testing.T, r *testRepo) string {
			elsewhere := filepath.Join(filepath.Dir(r.root), "metadata")
			if err := os.Rename(filepath.Join(r.root, ".git"), elsewhere); err != nil {
				t.Fatal(err)
			}
			symlink(t, elsewhere, filepath.Join(r.root, ".git"))
			return r.root
		}},
		{"dangling .git symlink", func(t *testing.T, r *testRepo) string {
			sub := filepath.Join(r.root, "sub")
			if err := os.Mkdir(sub, 0750); err != nil {
				t.Fatal(err)
			}
			symlink(t, filepath.Join(r.root, "missing"), filepath.Join(sub, ".git"))
			return sub
		}},
		{"empty .git directory", func(t *testing.T, r *testRepo) string {
			return filepath.Dir(r.write("sub/.git/placeholder", ""))
		}},
		{"invalid HEAD", func(t *testing.T, r *testRepo) string {
			r.write("sub/.git/HEAD", "garbage\n")
			r.write("sub/.git/objects/placeholder", "")
			r.write("sub/.git/refs/placeholder", "")
			return filepath.Join(r.root, "sub")
		}},
		{"detached HEAD", func(t *testing.T, r *testRepo) string {
			r.write("sub/.git/HEAD", strings.Repeat("0", 40)+"\n")
			r.write("sub/.git/objects/placeholder", "")
			r.write("sub/.git/refs/placeholder", "")
			return filepath.Join(r.root, "sub")
		}},
		{"symlinked HEAD", func(t *testing.T, r *testRepo) string {
			r.write("sub/.git/objects/placeholder", "")
			r.write("sub/.git/refs/placeholder", "")
			symlink(t, "refs/heads/main", filepath.Join(r.root, "sub", ".git", "HEAD"))
			return filepath.Join(r.root, "sub")
		}},
		{"HEAD symlink outside refs", func(t *testing.T, r *testRepo) string {
			r.write("sub/.git/objects/placeholder", "")
			r.write("sub/.git/refs/placeholder", "")
			symlink(t, "../HEAD", filepath.Join(r.root, "sub", ".git", "HEAD"))
			return filepath.Join(r.root, "sub")
		}},
		{"symlinked start directory", func(t *testing.T, r *testRepo) string {
			link := filepath.Join(filepath.Dir(r.root), "link")
			symlink(t, filepath.Dir(r.write("a/b/file", "")), link)
			return link
		}},
		{"symlink leaving the repository", func(t *testing.T, r *testRepo) string {
			link := filepath.Join(r.root, "out")
			symlink(t, r.mkdir("outside"), link)
			return link
		}},
		{"ceiling above the repository", func(t *testing.T, r *testRepo) string {
			t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(r.root))
			return filepath.Dir(r.write("a/b/file", ""))
		}},
		{"ceiling at the repository", func(t *testing.T, r *testRepo) string {
			t.Setenv("GIT_CEILING_DIRECTORIES", r.root)
			return filepath.Dir(r.write("a/b/file", ""))
		}},
		{"ceiling with a trailing separator", func(t *testing.T, r *testRepo) string {
			t.Setenv("GIT_CEILING_DIRECTORIES", r.root+string(filepath.Separator))
			return filepath.Dir(r.write("a/b/file", ""))
		}},
		{"ceiling at the start", func(t *testing.T, r *testRepo) string {
			start := filepath.Dir(r.write("a/b/file", ""))
			t.Setenv("GIT_CEILING_DIRECTORIES", start)
			return start
		}},
		{"ceiling list", func(t *testing.T, r *testRepo) string {
			t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Join(r.root, "a", "b")+string(filepath.ListSeparator)+"relative"+string(filepath.ListSeparator)+filepath.Join(r.root, "a"))
			return filepath.Dir(r.write("a/b/c/file", ""))
		}},
		{"ceiling through a symlink", func(t *testing.T, r *testRepo) string {
			link := filepath.Join(filepath.Dir(r.root), "link")
			symlink(t, r.root, link)
			t.Setenv("GIT_CEILING_DIRECTORIES", link)
			return filepath.Dir(r.write("a/b/file", ""))
		}},
		{"ceiling after an empty entry stays verbatim", func(t *testing.T, r *testRepo) string {
			link := filepath.Join(filepath.Dir(r.root), "link")
			symlink(t, r.root, link)
			t.Setenv("GIT_CEILING_DIRECTORIES", string(filepath.ListSeparator)+link)
			return filepath.Dir(r.write("a/b/file", ""))
		}},
		{"discovery across filesystems", func(t *testing.T, r *testRepo) string {
			t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "true")
			return filepath.Dir(r.write("a/b/file", ""))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireMatchesGit(t, tc.setup(t, newTestRepo(t)))
		})
	}
}

func TestOpenGitFileErrorsMatchGit(t *testing.T) {
	for name, content := range map[string]string{
		"no gitdir line":   "nonsense\n",
		"missing target":   "gitdir: elsewhere\n",
		"trailing spaces":  "gitdir: ../repo/.git  \n",
		"no space":         "gitdir:../repo/.git\n",
		"capitalised":      "Gitdir: ../repo/.git\n",
		"empty path":       "gitdir: \r\n",
		"empty file":       "",
		"too large":        "gitdir: ../repo/.git\n" + strings.Repeat("\n", 1<<20),
		"not a repository": "gitdir: ../repo\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			requireMatchesGit(t, r.gitFile("other", content))
			if _, outcome, _ := gitDiscovery(t, filepath.Join(filepath.Dir(r.root), "other")); outcome != gitFailed {
				t.Fatalf("git outcome = %d, want a failure", outcome)
			}
		})
	}
}

func TestOpenNonRegularGitFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes are not files on Windows")
	}
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo required")
	}
	r := newTestRepo(t)
	other := r.mkdir("other")
	if out, err := exec.CommandContext(t.Context(), "mkfifo", filepath.Join(other, ".git")).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	requireMatchesGit(t, other)
	if repo, err := Open(t.Context(), other); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Open = %+v, %v", repo, err)
	}
}

func TestOpenEmptyCommonDir(t *testing.T) {
	r := newTestRepo(t)
	worktree := r.worktree("wt")
	linked := requireMatchesGit(t, worktree)
	if err := os.WriteFile(filepath.Join(linked.GitDir, "commondir"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, outcome, _ := gitDiscovery(t, worktree); outcome != gitFailed {
		t.Fatalf("git outcome = %d, want a failure", outcome)
	}
	requireMatchesGit(t, worktree)
}

func TestOpenRepositoryFormatMatchesGit(t *testing.T) {
	for name, lines := range map[string]string{
		"version 0":                              "[core]\n\trepositoryformatversion = 0\n",
		"version 1":                              "[core]\n\trepositoryformatversion = 1\n",
		"version 2":                              "[core]\n\trepositoryformatversion = 2\n",
		"version with unit":                      "[core]\n\trepositoryformatversion = 1k\n",
		"hexadecimal version":                    "[core]\n\trepositoryformatversion = 0x1\n",
		"octal version":                          "[core]\n\trepositoryformatversion = 010\n",
		"textual version":                        "[core]\n\trepositoryformatversion = abc\n",
		"empty version":                          "[core]\n\trepositoryformatversion =\n",
		"unknown extension at version 0":         "[core]\n\trepositoryformatversion = 0\n[extensions]\n\tbogus = true\n",
		"unknown extension at version 1":         "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tbogus = true\n",
		"unknown extension at version -3":        "[core]\n\trepositoryformatversion = -3\n[extensions]\n\tbogus = true\n",
		"sha1 at version 1":                      "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectformat = sha1\n",
		"sha256 at version 0":                    "[core]\n\trepositoryformatversion = 0\n[extensions]\n\tobjectformat = sha256\n",
		"objectformat at version -1":             "[core]\n\trepositoryformatversion = -1\n[extensions]\n\tobjectformat = bogus\n",
		"uppercase objectformat":                 "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectformat = SHA1\n",
		"refstorage with payload":                "[core]\n\trepositoryformatversion = 1\n[extensions]\n\trefstorage = files://payload\n",
		"unknown refstorage":                     "[core]\n\trepositoryformatversion = 1\n[extensions]\n\trefstorage = bogus\n",
		"v1 extension at version 0":              "[core]\n\trepositoryformatversion = 0\n[extensions]\n\tnoop-v1 = true\n",
		"v1 extension at version 1":              "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tnoop-v1 = true\n",
		"v0 extensions at version 0":             "[core]\n\trepositoryformatversion = 0\n[extensions]\n\tnoop = true\n\tpartialclone = origin\n\tpreciousobjects = true\n",
		"bad preciousobjects":                    "[extensions]\n\tpreciousobjects = maybe\n",
		"bad worktreeconfig":                     "[extensions]\n\tworktreeconfig = maybe\n",
		"bad relativeworktrees":                  "[core]\n\trepositoryformatversion = 1\n[extensions]\n\trelativeworktrees = maybe\n",
		"v1 booleans":                            "[core]\n\trepositoryformatversion = 1\n[extensions]\n\trelativeworktrees = true\n\tsubmodulepathconfig = false\n",
		"duplicate compatobjectformat":           "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tcompatobjectformat = sha256\n\tcompatobjectformat = sha256\n",
		"bad compatobjectformat":                 "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tcompatobjectformat = md5\n",
		"bad core.bare":                          "[core]\n\tbare = maybe\n",
		"extension in a subsection at version 1": "[core]\n\trepositoryformatversion = 1\n[extensions \"sub\"]\n\tnoop = true\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := newTestRepo(t)
			config, err := os.OpenFile(filepath.Join(r.root, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := config.WriteString(lines); err != nil {
				t.Fatal(err)
			}
			if err := config.Close(); err != nil {
				t.Fatal(err)
			}
			requireMatchesGit(t, r.root)
		})
	}
}

func TestOpenWithoutConfigFile(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(filepath.Join(r.root, ".git", "config")); err != nil {
		t.Fatal(err)
	}
	requireMatchesGit(t, r.root)
}

func TestOpenHookEnvironment(t *testing.T) {
	r := newTestRepo(t)
	start := filepath.Dir(r.write("a/file", ""))
	want := requireMatchesGit(t, start)
	missing := filepath.Join(t.TempDir(), "missing")
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		t.Setenv(name, missing)
	}
	got, err := Open(t.Context(), start)
	if err != nil || got == nil || got.WorkTree != want.WorkTree || got.GitDir != want.GitDir || got.CommonDir != want.CommonDir {
		t.Fatalf("Open = %+v, %v, want %+v", got, err, want)
	}
}

func TestOpenBadDiscoveryEnvironment(t *testing.T) {
	r := newTestRepo(t)
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "maybe")
	if _, outcome, _ := gitDiscovery(t, r.root); outcome != gitFailed {
		t.Fatalf("git outcome = %d, want a failure", outcome)
	}
	requireMatchesGit(t, r.root)
}

func TestOpenStopsAtFilesystemBoundary(t *testing.T) {
	newTestRepo(t)
	start := resolvedPath(t, t.TempDir())
	device, err := deviceID(start)
	if err != nil {
		t.Fatal(err)
	}
	boundary := false
	for dir := start; filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if parent, err := deviceID(filepath.Dir(dir)); err != nil {
			t.Fatal(err)
		} else if parent != device {
			boundary = true
			break
		}
	}
	if !boundary {
		t.Skip("the temporary directory shares a filesystem with the root")
	}
	if _, outcome, stderr := gitDiscovery(t, start); outcome != gitAbsent || !strings.Contains(stderr, "mount point") {
		t.Fatalf("git outcome = %d: %s", outcome, stderr)
	}
	requireMatchesGit(t, start)
	t.Setenv("GIT_DISCOVERY_ACROSS_FILESYSTEM", "1")
	requireMatchesGit(t, start)
}

func TestParseInt(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{{"0", 0}, {" +1", 1}, {"-1", -1}, {"0x1F", 31}, {"017", 15}, {"2k", 2048}, {"1M", 1 << 20}, {"1g", 1 << 30}, {"-2g", -1 << 31}, {"2147483647", 1<<31 - 1}} {
		if got, ok := parseInt(tc.value); !ok || got != tc.want {
			t.Errorf("parseInt(%q) = %d, %v, want %d", tc.value, got, ok, tc.want)
		}
	}
	for _, value := range []string{"", " ", "x", "0x", "1_0", "08", "2g", "2147483648", "1kk", "1 k", "99999999999999999999"} {
		if got, ok := parseInt(value); ok {
			t.Errorf("parseInt(%q) = %d, want a failure", value, got)
		}
	}
}

func TestOpenReadsConfig(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "core.ignoreCase", "true")
	fixture.git("config", "core.precomposeUnicode", "true")
	fixture.git("config", "core.excludesFile", "~/global-ignore")
	repo := fixture.open()
	if !repo.IgnoreCase() {
		t.Fatal("IgnoreCase = false")
	}
	if !repo.PrecomposeUnicode() {
		t.Fatal("PrecomposeUnicode = false")
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
			repo := fixture.open()
			want := tc.want
			if want != "" {
				want = filepath.Join(repo.WorkTree, want)
			}
			if repo.excludes != want {
				t.Fatalf("excludes = %q, want %q", repo.excludes, want)
			}
		})
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
	if repo.GitDir != filepath.Join(resolvedPath(t, fixture.root), ".git") {
		t.Fatalf("GitDir = %q", repo.GitDir)
	}
	if got, err := repo.TrackedFiles(t.Context()); err != nil || len(got) != 1 {
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
			ignores, err := repo.Ignores(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !ignores.Ignored("ignored", true) {
				t.Fatal("Ignored(ignored) = false")
			}
			if _, err := repo.TrackedFiles(t.Context()); err != nil {
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

func (r *testRepo) gitExit(args ...string) int {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	r.t.Fatalf("git %v: %v", args, err)
	return -1
}

func (r *testRepo) probe(key string) int {
	r.t.Helper()
	repo, err := Open(r.t.Context(), r.root)
	if err != nil {
		return 128
	}
	settings, err := repo.config(r.t.Context())
	switch _, ok := settings.last(key); {
	case err != nil:
		return 128
	case ok:
		return 0
	}
	return 1
}

func (r *testRepo) compareProbe(key string) {
	r.t.Helper()
	ours, theirs := r.probe(key), r.gitExit("config", "--get", key)
	if ours != theirs {
		r.t.Fatalf("%s: ours %d, git %d", key, ours, theirs)
	}
}

func TestOpenConfigChainOrderMatchesGit(t *testing.T) {
	fixture := newTestRepo(t)
	system := filepath.Join(fixture.home, "system")
	writeConfig(t, system, "[core]\n\torder = system\n")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	t.Setenv("GIT_CONFIG_SYSTEM", system)
	writeConfig(t, filepath.Join(fixture.home, ".config", "git", "config"), "[core]\n\torder = xdg\n")
	writeConfig(t, filepath.Join(fixture.home, ".gitconfig"), "[core]\n\torder = home\n")
	fixture.git("config", "core.repositoryFormatVersion", "1")
	fixture.git("config", "extensions.worktreeConfig", "true")
	fixture.git("config", "--add", "core.order", "repository")
	fixture.git("config", "--worktree", "core.order", "worktree")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.order")
	t.Setenv("GIT_CONFIG_VALUE_0", "count")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.order'='parameters'")
	var ours []string
	for _, value := range fixture.settings()["core.order"] {
		ours = append(ours, value.text)
	}
	theirs := strings.Fields(fixture.git("config", "--get-all", "core.order"))
	want := []string{"system", "xdg", "home", "repository", "worktree", "count", "parameters"}
	if !slices.Equal(ours, want) || !slices.Equal(theirs, want) {
		t.Fatalf("order: ours %q, git %q, want %q", ours, theirs, want)
	}
}

func TestOpenEmptyConfigEnvironmentMatchesGit(t *testing.T) {
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			writeConfig(t, filepath.Join(fixture.home, ".gitconfig"), "[core]\n\tprobe = 1\n")
			writeConfig(t, filepath.Join(fixture.home, ".config", "git", "config"), "[core]\n\tprobe = 1\n")
			if name == "GIT_CONFIG_SYSTEM" {
				t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
				t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(fixture.home, ".gitconfig"))
				t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
			}
			fixture.compareProbe("core.probe")
			if got := fixture.probe("core.probe"); got != 0 {
				t.Fatalf("probe = %d, want 0", got)
			}
			t.Setenv(name, "")
			fixture.compareProbe("core.probe")
			if got := fixture.probe("core.probe"); got != 1 {
				t.Fatalf("probe = %d, want 1", got)
			}
		})
	}
}

func TestOpenSkipsUnusableConfigFilesLikeGit(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		fixture := newTestRepo(t)
		fixture.git("config", "core.probe", "1")
		if err := os.MkdirAll(filepath.Join(fixture.home, ".config", "git", "config"), 0750); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
		t.Setenv("GIT_CONFIG_SYSTEM", fixture.home)
		fixture.compareProbe("core.probe")
		if got := fixture.probe("core.probe"); got != 0 {
			t.Fatalf("probe = %d, want 0", got)
		}
	})
	t.Run("unreadable global", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs file permissions that bind the current user")
		}
		fixture := newTestRepo(t)
		global := filepath.Join(fixture.home, ".gitconfig")
		writeConfig(t, global, "[core]\n\tprobe = 1\n")
		if err := os.Chmod(global, 0); err != nil {
			t.Fatal(err)
		}
		fixture.git("config", "core.other", "1")
		fixture.compareProbe("core.other")
		fixture.compareProbe("core.probe")
	})
	t.Run("bad GIT_CONFIG_NOSYSTEM", func(t *testing.T) {
		fixture := newTestRepo(t)
		t.Setenv("GIT_CONFIG_NOSYSTEM", "maybe")
		fixture.compareProbe("core.bare")
	})
}

func probeIn(t *testing.T, dir, key string) int {
	t.Helper()
	repo, err := Open(t.Context(), dir)
	if err != nil || repo == nil {
		return 128
	}
	settings, err := repo.config(t.Context())
	switch _, ok := settings.last(key); {
	case err != nil:
		return 128
	case ok:
		return 0
	}
	return 1
}

func compareProbeIn(t *testing.T, dir, key string, want int) {
	t.Helper()
	ours := probeIn(t, dir, key)
	_, _, err := gitIn(t, dir, "config", "--get", key)
	theirs := 0
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		theirs = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if ours != theirs || ours != want {
		t.Fatalf("%s from %s: ours %d, git %d, want %d", key, dir, ours, theirs, want)
	}
}

func TestOpenRelativeConfigPathsMatchGit(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		file string
	}{
		"xdg":    {env: map[string]string{"XDG_CONFIG_HOME": "xdg"}, file: "xdg/git/config"},
		"home":   {env: map[string]string{"HOME": "home", "USERPROFILE": "home", "XDG_CONFIG_HOME": ""}, file: "home/.gitconfig"},
		"global": {env: map[string]string{"GIT_CONFIG_GLOBAL": "global"}, file: "global"},
		"system": {env: map[string]string{"GIT_CONFIG_NOSYSTEM": "0", "GIT_CONFIG_SYSTEM": "system"}, file: "system"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			sub := filepath.Join(fixture.root, "sub")
			if err := os.MkdirAll(sub, 0o750); err != nil {
				t.Fatal(err)
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
				if value == "" {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
			}
			writeConfig(t, filepath.Join(fixture.root, filepath.FromSlash(tc.file)), "[core]\n\tprobe = 1\n")
			t.Chdir(fixture.root)
			compareProbeIn(t, fixture.root, "core.probe", 0)
			t.Chdir(sub)
			compareProbeIn(t, sub, "core.probe", 0)
			if err := os.Remove(filepath.Join(fixture.root, filepath.FromSlash(tc.file))); err != nil {
				t.Fatal(err)
			}
			writeConfig(t, filepath.Join(sub, filepath.FromSlash(tc.file)), "[core]\n\tprobe = 1\n")
			compareProbeIn(t, sub, "core.probe", 1)
		})
	}
}

func (r *testRepo) gitKnowsWorktreeConditions() bool {
	r.t.Helper()
	writeConfig(r.t, filepath.Join(r.home, "worktree-known"), "[core]\n\tworktreeKnown = 1\n")
	command := exec.CommandContext(r.t.Context(), "git", "config", "--get", "core.worktreeKnown")
	command.Dir = r.root
	command.Env = append(os.Environ(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=includeIf.worktree:**.path", "GIT_CONFIG_VALUE_0="+filepath.ToSlash(filepath.Join(r.home, "worktree-known")))
	return command.Run() == nil
}

func TestOpenCommandConfigIncludesMatchGit(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"absolute include":   {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"home include":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "~/inc"},
		"relative include":   {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "include.path", "GIT_CONFIG_VALUE_0": "inc"},
		"bare include":       {"GIT_CONFIG_PARAMETERS": "'include.path'"},
		"dot gitdir":         {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:./elsewhere/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"matching gitdir":    {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:repo/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"other gitdir":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.gitdir:other/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"bare false include": {"GIT_CONFIG_PARAMETERS": "'includeIf.onbranch:nope.path'"},
		"dot worktree":       {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.worktree:./elsewhere/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"worktree":           {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.worktree:{root}.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"slashed worktree":   {"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "includeIf.worktree:{root}/.path", "GIT_CONFIG_VALUE_0": "{home}/inc"},
		"worktree glob":      {"GIT_CONFIG_PARAMETERS": "'includeIf.worktree:**/repo.path'='{home}/inc'"},
		"remote url":         {"GIT_CONFIG_PARAMETERS": "'remote.origin.url'='https://example.com/x' 'includeIf.hasconfig:remote.*.url:https://example.com/**.path'='{home}/inc'"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			writeConfig(t, filepath.Join(fixture.home, "inc"), "[core]\n\tinc = 1\n")
			for key, value := range env {
				value = strings.ReplaceAll(value, "{home}", filepath.ToSlash(fixture.home))
				t.Setenv(key, strings.ReplaceAll(value, "{root}", filepath.ToSlash(resolvedPath(t, fixture.root))))
			}
			want, pinned := map[string]int{"dot worktree": 0, "worktree": 0, "slashed worktree": 1, "worktree glob": 0}[name]
			if pinned && !fixture.gitKnowsWorktreeConditions() {
				if got := fixture.probe("core.inc"); got != want {
					t.Fatalf("probe = %d, want %d", got, want)
				}
				return
			}
			fixture.compareProbe("core.inc")
			if pinned && fixture.probe("core.inc") != want {
				t.Fatalf("probe = %d, want %d", fixture.probe("core.inc"), want)
			}
		})
	}
}

func TestOpenObjectFormatMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		global, setup string
	}{
		"global object format":      {global: "[extensions]\n\tobjectFormat = sha256\n"},
		"global worktree config":    {global: "[extensions]\n\tworktreeConfig = true\n", setup: "worktree"},
		"version absent":            {setup: "unversioned"},
		"upper case object format":  {setup: "upper"},
		"bare object format":        {setup: "bare"},
		"bad version":               {setup: "badversion"},
		"versioned worktree config": {setup: "versioned"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write("file", "")
			fixture.git("add", "file")
			if tc.global != "" {
				global := filepath.Join(fixture.home, "gitconfig")
				writeConfig(t, global, tc.global)
				t.Setenv("GIT_CONFIG_GLOBAL", global)
			}
			config := filepath.Join(fixture.root, ".git", "config")
			worktree := filepath.Join(fixture.root, ".git", "config.worktree")
			switch tc.setup {
			case "worktree":
				writeConfig(t, worktree, "[core]\n\tprobe = 1\n")
			case "versioned":
				writeConfig(t, worktree, "[core]\n\tprobe = 1\n")
				fixture.git("config", "core.repositoryFormatVersion", "+1")
				fixture.git("config", "extensions.worktreeConfig", "yes")
			case "unversioned":
				fixture.git("config", "--unset", "core.repositoryFormatVersion")
				fixture.git("config", "extensions.objectFormat", "sha256")
			case "upper":
				fixture.git("config", "core.repositoryFormatVersion", "1")
				fixture.git("config", "extensions.objectFormat", "SHA256")
			case "bare":
				fixture.git("config", "core.repositoryFormatVersion", "1")
				data, err := os.ReadFile(config)
				if err != nil {
					t.Fatal(err)
				}
				writeConfig(t, config, string(data)+"[extensions]\n\tobjectFormat\n")
			case "badversion":
				fixture.git("config", "core.repositoryFormatVersion", "one")
			}
			command := exec.CommandContext(t.Context(), "git", "rev-parse", "--show-object-format")
			command.Dir = fixture.root
			out, gitErr := command.Output()
			repo, err := Open(t.Context(), fixture.root)
			if (err == nil) != (gitErr == nil) {
				t.Fatalf("Open error %v, git error %v", err, gitErr)
			}
			if err != nil {
				return
			}
			want := map[string]int{"sha1": 20, "sha256": 32}[strings.TrimSpace(string(out))]
			if repo.hashLength != want {
				t.Fatalf("hashLength = %d, git says %s", repo.hashLength, out)
			}
			if got, err := repo.TrackedFiles(t.Context()); err != nil || !slices.Equal(got, []string{"file"}) {
				t.Fatalf("TrackedFiles = %q, %v", got, err)
			}
			fixture.compareProbe("core.probe")
		})
	}
}

func TestOpenExcludesFileMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		global, local string
	}{
		"bare":                   {local: "[core]\n\texcludesFile\n"},
		"bare overridden":        {global: "[core]\n\texcludesFile\n", local: "[core]\n\texcludesFile = mine\n"},
		"optional missing":       {local: "[core]\n\texcludesFile = :(optional)missing\n"},
		"optional present":       {local: "[core]\n\texcludesFile = :(optional)mine\n"},
		"optional after present": {global: "[core]\n\texcludesFile = mine\n", local: "[core]\n\texcludesFile = :(optional)missing\n"},
		"unknown user":           {local: "[core]\n\texcludesFile = ~nobody-gitrepo-test/x\n"},
		"home":                   {local: "[core]\n\texcludesFile = ~/home-ignore\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write("mine", "from-mine\n")
			writeConfig(t, filepath.Join(fixture.home, ".config", "git", "ignore"), "from-xdg\n")
			writeConfig(t, filepath.Join(fixture.home, "home-ignore"), "from-home\n")
			global := filepath.Join(fixture.home, "gitconfig")
			writeConfig(t, global, tc.global)
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			data, err := os.ReadFile(filepath.Join(fixture.root, ".git", "config"))
			if err != nil {
				t.Fatal(err)
			}
			writeConfig(t, filepath.Join(fixture.root, ".git", "config"), string(data)+tc.local)
			for _, file := range []string{"from-mine", "from-xdg", "from-home"} {
				theirs := fixture.gitExit("check-ignore", "--quiet", "--no-index", file)
				ours := 128
				if repo, err := Open(t.Context(), fixture.root); err == nil {
					ignores, err := repo.Ignores(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					ours = map[bool]int{true: 0, false: 1}[ignores.Ignored(file, false)]
				}
				if ours != theirs {
					t.Fatalf("%s: ours %d, git %d", file, ours, theirs)
				}
			}
		})
	}
}

func TestLookupHome(t *testing.T) {
	dir := t.TempDir()
	drive := filepath.VolumeName(dir)
	path := dir[len(drive):]
	for _, tc := range []struct {
		name    string
		windows bool
		env     map[string]string
		want    string
		ok      bool
	}{
		{"home", false, map[string]string{"HOME": "/h", "USERPROFILE": "/u"}, "/h", true},
		{"empty home", false, map[string]string{"HOME": ""}, "", true},
		{"unset on unix", false, map[string]string{"USERPROFILE": "/u"}, "", false},
		{"home on windows", true, map[string]string{"HOME": "/h", "USERPROFILE": "/u"}, "/h", true},
		{"home drive and path", true, map[string]string{"HOMEDRIVE": drive, "HOMEPATH": path, "USERPROFILE": "/u"}, dir, true},
		{"missing home path", true, map[string]string{"HOMEDRIVE": drive, "HOMEPATH": path + "-missing", "USERPROFILE": "/u"}, "/u", true},
		{"drive without path", true, map[string]string{"HOMEDRIVE": drive, "USERPROFILE": "/u"}, "/u", true},
		{"nothing on windows", true, nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lookupHome(tc.windows, func(name string) (string, bool) {
				value, ok := tc.env[name]
				return value, ok
			})
			if got != tc.want || ok != tc.ok {
				t.Fatalf("lookupHome = %q, %v, want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ path, want string }{
		{"%(prefix)/etc/x", gitPrefix() + "/etc/x"},
		{"%(prefix)//abs", "/abs"},
		{"~", filepath.ToSlash(home)},
		{"~/x", filepath.ToSlash(home) + "/x"},
		{"x/~", "x/~"},
	} {
		if got, ok := expandHome(tc.path, false); !ok || got != tc.want {
			t.Errorf("expandHome(%q) = %q, %v, want %q", tc.path, got, ok, tc.want)
		}
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, ok := xdgConfigHome(); !ok || got != "/.config" {
		t.Fatalf("xdgConfigHome with empty HOME = %q, %v", got, ok)
	}
}

func TestReadFiles(t *testing.T) {
	fixture := newTestRepo(t)
	included := filepath.Join(fixture.home, "included")
	writeConfig(t, included, "[core]\n\tincluded = 1\n")
	global := filepath.Join(fixture.home, "gitconfig")
	writeConfig(t, global, "[include]\n\tpath = included\n\tpath = missing\n[core]\n\texcludesFile = ~/excludes\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	excludes := filepath.Join(fixture.home, "excludes")
	writeConfig(t, excludes, "excluded/\n")
	writeConfig(t, filepath.Join(fixture.root, ".git", "info", "exclude"), "info/\n")
	fixture.write(".gitignore", "ignored/\n")
	fixture.write("sub/.gitignore", "local/\n")
	fixture.write("sub/file", "")
	fixture.git("add", "sub/file")
	fixture.git("update-index", "--split-index")
	shared, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
	if err != nil || len(shared) != 1 {
		t.Fatalf("shared index: %q, %v", shared, err)
	}
	repo := fixture.open()
	ignores, err := repo.Ignores(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ignores.Ignored("sub/file", false)
	if _, err := repo.Index(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{global, included, filepath.Join(repo.GitDir, "HEAD"), filepath.Join(repo.GitDir, "config"), excludes, filepath.Join(repo.CommonDir, "info", "exclude"), filepath.Join(repo.WorkTree, ".gitignore"), filepath.Join(repo.WorkTree, "sub", ".gitignore"), repo.IndexPath(), filepath.Join(repo.GitDir, filepath.Base(shared[0]))}
	slices.Sort(want)
	if got := repo.ReadFiles(); !slices.Equal(got, want) {
		t.Fatalf("ReadFiles = %q, want %q", got, want)
	}
}

func TestSystemConfigPathMatchesGit(t *testing.T) {
	requireGit(t)
	for _, name := range []string{"GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.CommandContext(t.Context(), "git", "var", "GIT_CONFIG_SYSTEM").Output()
	if err != nil {
		t.Fatal(err)
	}
	theirs := filepath.Clean(filepath.FromSlash(strings.TrimSpace(string(out))))
	if ours := filepath.Clean(systemConfigPath()); ours != theirs {
		t.Fatalf("systemConfigPath = %q, git var GIT_CONFIG_SYSTEM = %q", ours, theirs)
	}
}

func TestGitPrefixWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := gitPrefix(); got != "/usr" {
		t.Fatalf("gitPrefix = %q", got)
	}
	if got := systemConfigPath(); got != filepath.FromSlash("/etc/gitconfig") {
		t.Fatalf("systemConfigPath = %q", got)
	}
}

func TestInstallationOf(t *testing.T) {
	for _, tc := range []struct{ goos, exe, prefix, config string }{
		{"linux", "/usr/bin/git", "/usr", "/etc/gitconfig"},
		{"linux", "/usr/local/bin/git", "/usr/local", "/usr/local/etc/gitconfig"},
		{"darwin", "/usr/bin/git", "/usr", "/etc/gitconfig"},
		{"darwin", "/opt/homebrew/Cellar/git/2.55.0/bin/git", "/opt/homebrew/opt/git", "/opt/homebrew/etc/gitconfig"},
		{"linux", "/home/linuxbrew/.linuxbrew/Cellar/git/2.55.0/bin/git", "/home/linuxbrew/.linuxbrew/opt/git", "/home/linuxbrew/.linuxbrew/etc/gitconfig"},
		{"darwin", "/opt/local/bin/git", "/opt/local", "/opt/local/etc/gitconfig"},
		{"linux", "/nix/store/abc-git-2.55.0/bin/git", "/nix/store/abc-git-2.55.0", "/etc/gitconfig"},
		{"darwin", "/nix/store/abc-git-2.55.0/bin/git", "/nix/store/abc-git-2.55.0", "/nix/store/abc-git-2.55.0/etc/gitconfig"},
		{"windows", "C:/Program Files/Git/cmd/git.exe", "C:/Program Files/Git", "C:/Program Files/Git/etc/gitconfig"},
		{"windows", "C:/Program Files/Git/mingw64/bin/git.exe", "C:/Program Files/Git", "C:/Program Files/Git/etc/gitconfig"},
		{"windows", "C:/Program Files/Git/clangarm64/bin/git.exe", "C:/Program Files/Git", "C:/Program Files/Git/etc/gitconfig"},
		{"linux", "/opt/mingw64/bin/git", "/opt/mingw64", "/opt/mingw64/etc/gitconfig"},
	} {
		got := installationOf(tc.goos, tc.exe)
		if got.prefix != tc.prefix || got.systemConfig != filepath.FromSlash(tc.config) {
			t.Errorf("installationOf(%s, %q) = %+v, want prefix %q and config %q", tc.goos, tc.exe, got, tc.prefix, tc.config)
		}
	}
}

func TestGitInstallationResolvesSymlinks(t *testing.T) {
	target := filepath.Join(t.TempDir(), "usr", "bin", "git")
	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(t.TempDir(), "custom", "bin")
	if err := os.MkdirAll(custom, 0750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(custom, "git")
	if runtime.GOOS == "windows" {
		link += ".exe"
		target += ".exe"
		if err := os.Rename(filepath.Join(filepath.Dir(target), "git"), target); err != nil {
			t.Fatal(err)
		}
	}
	symlink(t, target, link)
	t.Setenv("PATH", custom)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := gitPrefix(), filepath.ToSlash(filepath.Dir(filepath.Dir(resolved))); got != want {
		t.Fatalf("gitPrefix = %q, want %q", got, want)
	}
}

func TestReadFilesLocators(t *testing.T) {
	fixture := newTestRepo(t)
	worktree := fixture.worktree("wt")
	repo, err := Open(t.Context(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Index(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := repo.ReadFiles()
	for _, want := range []string{filepath.Join(resolvedPath(t, worktree), ".git"), filepath.Join(repo.GitDir, "commondir")} {
		if !slices.Contains(got, want) {
			t.Errorf("ReadFiles = %q, missing %q", got, want)
		}
	}
}

func TestOpenEmptyHomeMatchesGit(t *testing.T) {
	fixture := newTestRepo(t)
	writeConfig(t, filepath.Join(fixture.root, ".gitconfig"), "[core]\n\tprobe = 1\n")
	writeConfig(t, filepath.Join(fixture.root, ".config", "git", "config"), "[core]\n\tprobe = 1\n")
	t.Chdir(fixture.root)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
		t.Fatal(err)
	}
	compareProbeIn(t, fixture.root, "core.probe", 1)
	if got := globalConfigFiles(); !slices.Equal(got, []string{filepath.FromSlash("/.config/git/config"), filepath.FromSlash("/.gitconfig")}) {
		t.Fatalf("globalConfigFiles = %q", got)
	}
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatal(err)
	}
	compareProbeIn(t, fixture.root, "core.probe", 1)
	if got := globalConfigFiles(); runtime.GOOS != "windows" && len(got) != 0 {
		t.Fatalf("globalConfigFiles without HOME = %q", got)
	}
}

func TestOpenRelativeConfigPathsBareMatchGit(t *testing.T) {
	fixture := newTestRepo(t, "--bare")
	t.Setenv("GIT_CONFIG_GLOBAL", "global")
	writeConfig(t, filepath.Join(fixture.root, "global"), "[core]\n\tprobe = 1\n")
	t.Chdir(fixture.root)
	compareProbeIn(t, fixture.root, "core.probe", 0)
	hooks := filepath.Join(fixture.root, "hooks")
	t.Chdir(hooks)
	compareProbeIn(t, hooks, "core.probe", 1)
	writeConfig(t, filepath.Join(hooks, "global"), "[core]\n\tprobe = 1\n")
	compareProbeIn(t, hooks, "core.probe", 0)
}

func TestOpenBackslashExcludesFileMatchesGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash cannot appear in a Windows file name")
	}
	for _, name := range []string{`\ignore`, "C:ignore"} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write(name, "probe.txt\n")
			fixture.write("sub/probe.txt", "")
			fixture.git("config", "core.excludesFile", name)
			t.Chdir(fixture.home)
			ignores, err := fixture.open().Ignores(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			ours := ignores.Ignored("sub/probe.txt", false)
			if theirs := fixture.gitBool("check-ignore", "-q", "sub/probe.txt"); ours != theirs {
				t.Fatalf("ignored by us %v, by git %v", ours, theirs)
			}
			if !ours {
				t.Fatal("the relative excludes file was not read")
			}
		})
	}
}

func TestOpenNegativeFormatVersionsMatchGit(t *testing.T) {
	for name, tc := range map[string]struct {
		version  string
		bare     bool
		worktree bool
	}{
		"minus two bare":     {version: "-2", bare: true},
		"minus two worktree": {version: "-2", worktree: true},
		"minus one bare":     {version: "-1", bare: true},
		"minus one worktree": {version: "-1", worktree: true},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			elsewhere := filepath.Join(fixture.home, "elsewhere")
			if err := os.MkdirAll(elsewhere, 0o750); err != nil {
				t.Fatal(err)
			}
			fixture.git("config", "core.repositoryformatversion", tc.version)
			if tc.bare {
				fixture.git("config", "core.bare", "true")
			}
			if tc.worktree {
				fixture.git("config", "core.worktree", filepath.ToSlash(elsewhere))
			}
			repo := fixture.open()
			if bare := repo.WorkTree == ""; tc.version == "-2" && bare != tc.bare {
				t.Fatalf("bare = %v, want %v", bare, tc.bare)
			}
			if tc.version == "-2" && tc.worktree && !os.SameFile(statPath(t, repo.WorkTree), statPath(t, elsewhere)) {
				t.Fatalf("work tree = %s, want %s", repo.WorkTree, elsewhere)
			}
			if tc.version == "-2" && !gitAtLeast(t, 2, 56) {
				return
			}
			theirsBare, _, err := gitIn(t, fixture.root, "rev-parse", "--is-bare-repository")
			if err != nil {
				t.Fatal(err)
			}
			if ours := repo.WorkTree == ""; ours != (theirsBare == "true") {
				t.Fatalf("bare: ours %v, git %s", ours, theirsBare)
			}
			if repo.WorkTree == "" {
				return
			}
			theirs, _, err := gitIn(t, fixture.root, "rev-parse", "--show-toplevel")
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(statPath(t, repo.WorkTree), statPath(t, theirs)) {
				t.Fatalf("work tree: ours %s, git %s", repo.WorkTree, theirs)
			}
		})
	}
}

func statPath(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func gitAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	version, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "git version ")
	if !ok {
		t.Fatalf("git --version = %q", out)
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		t.Fatalf("git --version = %q", out)
	}
	gotMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	gotMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	return gotMajor > major || gotMajor == major && gotMinor >= minor
}

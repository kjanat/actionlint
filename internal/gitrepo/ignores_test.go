package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestIgnoresPrecedence(t *testing.T) {
	fixture := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(fixture.home, ".config", "git", "ignore"), []byte("global/\nshared.txt\n!keep-global.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture.write(".git/info/exclude", "excluded/\n!shared.txt\nkeep-global.txt\n")
	fixture.write(".gitignore", "ignored/\n*.log\n!keep.log\nsub/deep/\n")
	fixture.write("sub/.gitignore", "!*.log\nlocal/\n")
	fixture.write("sub/nested/.gitignore", "keep.log\n")
	fixture.write("ignored/.gitignore", "!inside.txt\n")
	ignores, err := fixture.open().Ignores()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"global", true, true},
		{"a/global", true, true},
		{"shared.txt", false, false},
		{"keep-global.txt", false, true},
		{"excluded", true, true},
		{"ignored", true, true},
		{"ignored/inside.txt", false, true},
		{"ignored/x/y.txt", false, true},
		{"a.log", false, true},
		{"keep.log", false, false},
		{"sub/a.log", false, false},
		{"sub/local", true, true},
		{"sub/local/file.txt", false, true},
		{"sub/deep", true, true},
		{"sub/nested/keep.log", false, true},
		{"sub/nested/other.log", false, false},
		{"plain.txt", false, false},
		{"sub", true, false},
	} {
		got, err := ignores.Ignored(tc.path, tc.isDir)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Ignored(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
		}
		want := fixture.checkIgnore(tc.path, tc.isDir)
		if got != want {
			t.Errorf("Ignored(%q, %v) = %v, git says %v", tc.path, tc.isDir, got, want)
		}
	}
}

func TestIgnoresSkipSymlinkedFiles(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("rules", "linked/\n")
	if err := os.Symlink("rules", filepath.Join(fixture.root, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fixture.write("sub/rules", "deep/\n")
	if err := os.Symlink("rules", filepath.Join(fixture.root, "sub", ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ignores, err := fixture.open().Ignores()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"linked", "sub/deep"} {
		got, err := ignores.Ignored(path, true)
		if err != nil {
			t.Fatal(err)
		}
		if want := fixture.checkIgnore(path, true); got != want || got {
			t.Errorf("Ignored(%q) = %v, git says %v", path, got, want)
		}
	}
}

func TestIgnoresCaseFolding(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "core.ignoreCase", "true")
	fixture.write(".gitignore", "BUILD/\n")
	ignores, err := fixture.open().Ignores()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ignores.Ignored("build", true); err != nil || !got {
		t.Fatalf("Ignored(build) = %v, %v", got, err)
	}
}

func TestIgnoresFollowSymlinkedExcludeFiles(t *testing.T) {
	fixture := newTestRepo(t)
	exclude := fixture.write("rules/exclude", "from-exclude/\n")
	global := fixture.write("rules/global", "from-global/\n")
	fixture.symlink(exclude, ".git/info/exclude")
	fixture.symlink(global, filepath.Join(fixture.home, ".config", "git", "ignore"))
	fixture.assertIgnoresMatchGit(true, ignoreQuery{"from-exclude", true}, ignoreQuery{"from-global", true}, ignoreQuery{"other", true})
}

func TestIgnoresSkipDanglingExcludeFiles(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write(".gitignore", "kept\n")
	fixture.symlink(filepath.Join(fixture.root, "missing-exclude"), ".git/info/exclude")
	fixture.symlink(filepath.Join(fixture.root, "missing-global"), filepath.Join(fixture.home, ".config", "git", "ignore"))
	fixture.assertIgnoresMatchGit(true, ignoreQuery{"kept", false}, ignoreQuery{"other", false})
}

func TestIgnoresEmptyFiles(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write(".git/info/exclude", "")
	fixture.write(".gitignore", "kept\n")
	fixture.write("sub/.gitignore", "")
	if err := os.WriteFile(filepath.Join(fixture.home, ".config", "git", "ignore"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.assertIgnoresMatchGit(true, ignoreQuery{"kept", false}, ignoreQuery{"sub/kept", false}, ignoreQuery{"sub/other", false})
}

func TestIgnoresSkipDirectoryGitignore(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write(".git/info/exclude", "excluded\n")
	fixture.write(".gitignore/rules", "x\n")
	fixture.write("sub/.gitignore/rules", "y\n")
	if err := os.MkdirAll(filepath.Join(fixture.root, "empty", ".gitignore"), 0750); err != nil {
		t.Fatal(err)
	}
	fixture.assertIgnoresMatchGit(true,
		ignoreQuery{"x", false},
		ignoreQuery{"sub/y", false},
		ignoreQuery{"empty/x", false},
		ignoreQuery{"sub/excluded", false},
		ignoreQuery{"empty/excluded", false},
	)
}

func TestIgnoresDirectoryExcludeFile(t *testing.T) {
	for _, name := range []string{"exclude", "global"} {
		for _, populated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s populated %v", name, populated), func(t *testing.T) {
				fixture := newTestRepo(t)
				fixture.write(".gitignore", "kept\n")
				path := filepath.Join(fixture.root, ".git", "info", "exclude")
				if name == "global" {
					path = filepath.Join(fixture.home, ".config", "git", "ignore")
				}
				fixture.removeFile(path)
				if err := os.MkdirAll(path, 0750); err != nil {
					t.Fatal(err)
				}
				if populated {
					if err := os.WriteFile(filepath.Join(path, "rules"), []byte("other\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				fixture.assertIgnoresMatchGit(true, ignoreQuery{"kept", false}, ignoreQuery{"other", false})
			})
		}
	}
}

func TestIgnoresUnreadableFiles(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write(".gitignore", "kept\n")
	paths := []string{
		fixture.write("sub/.gitignore", "x\n"),
		fixture.write(".git/info/exclude", "y\n"),
		filepath.Join(fixture.home, ".config", "git", "ignore"),
	}
	if err := os.WriteFile(paths[2], []byte("z\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(path, 0600); err != nil {
				t.Error(err)
			}
		})
		if file, err := os.Open(path); err == nil {
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			t.Skip("permissions are not enforced")
		}
	}
	fixture.assertIgnoresMatchGit(true, ignoreQuery{"kept", false}, ignoreQuery{"sub/x", false}, ignoreQuery{"y", false}, ignoreQuery{"z", false})
}

func TestIgnoresSocketExcludeFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("access(2) applies to Unix only")
	}
	fixture := newTestRepo(t)
	fixture.write(".gitignore", "kept\n")
	path := filepath.Join(fixture.home, ".config", "git", "ignore")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", path)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.assertIgnoresMatchGit(true, ignoreQuery{"kept", false})
}

func TestIgnoresPatternFileSizeLimit(t *testing.T) {
	for _, size := range []int64{maxPatternFileSize - 1, maxPatternFileSize} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			fixture := newTestRepo(t)
			for _, name := range []string{"sub/.gitignore", ".git/info/exclude"} {
				path := fixture.write(name, "large\n#")
				if err := os.Truncate(path, size); err != nil {
					t.Fatal(err)
				}
			}
			fixture.assertIgnoresMatchGit(true, ignoreQuery{"sub/large", false}, ignoreQuery{"large", false})
		})
		t.Run(fmt.Sprintf("%d per directory only", size), func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.write(".gitignore", "kept\n")
			path := fixture.write("sub/.gitignore", "large\n#")
			if err := os.Truncate(path, size); err != nil {
				t.Fatal(err)
			}
			fixture.assertIgnoresMatchGit(true, ignoreQuery{"sub/large", false}, ignoreQuery{"kept", false})
		})
	}
}

type ignoreQuery struct {
	path  string
	isDir bool
}

func (r *testRepo) assertIgnoresMatchGit(mustMatch bool, queries ...ignoreQuery) {
	r.t.Helper()
	ignores, err := r.open().Ignores()
	matched := false
	for _, q := range queries {
		want, gitErr := r.gitCheckIgnore(q.path, q.isDir)
		if (err != nil) != (gitErr != nil) {
			r.t.Fatalf("Ignores() error: %v; git check-ignore %q error: %v", err, q.path, gitErr)
		}
		if err != nil {
			continue
		}
		got, err := ignores.Ignored(q.path, q.isDir)
		if err != nil {
			r.t.Fatal(err)
		}
		if got != want {
			r.t.Errorf("Ignored(%q, %v) = %v, git says %v", q.path, q.isDir, got, want)
		}
		matched = matched || got
	}
	if mustMatch && err == nil && !matched {
		r.t.Errorf("no query was ignored")
	}
}

func (r *testRepo) symlink(target, name string) {
	r.t.Helper()
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.root, filepath.FromSlash(name))
	}
	r.removeFile(path)
	if err := os.Symlink(target, path); err != nil {
		r.t.Skipf("symlinks unavailable: %v", err)
	}
}

func (r *testRepo) removeFile(path string) {
	r.t.Helper()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		r.t.Fatal(err)
	}
}

func (r *testRepo) checkIgnore(path string, isDir bool) bool {
	r.t.Helper()
	ignored, err := r.gitCheckIgnore(path, isDir)
	if err != nil {
		r.t.Fatal(err)
	}
	return ignored
}

func (r *testRepo) gitCheckIgnore(path string, isDir bool) (bool, error) {
	r.t.Helper()
	query := path
	if isDir {
		query += "/"
	}
	command := exec.CommandContext(r.t.Context(), "git", "check-ignore", "--no-index", "-q", "--", query)
	command.Dir = r.root
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git check-ignore %q: %w: %s", query, err, stderr.Bytes())
}

package gitrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestIgnoresUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced")
	}
	fixture := newTestRepo(t)
	path := fixture.write("sub/.gitignore", "x\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	})
	ignores, err := fixture.open().Ignores()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() != 0 {
		t.Skip("permissions are not enforced")
	}
	if _, err := ignores.Ignored("sub/x", false); err == nil {
		t.Fatal("an unreadable .gitignore was ignored")
	}
}

func (r *testRepo) checkIgnore(path string, isDir bool) bool {
	r.t.Helper()
	query := path
	if isDir {
		query += "/"
	}
	command := exec.CommandContext(r.t.Context(), "git", "check-ignore", "--no-index", "-q", "--", query)
	command.Dir = r.root
	err := command.Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false
	}
	r.t.Fatalf("git check-ignore %q: %v", query, err)
	return false
}

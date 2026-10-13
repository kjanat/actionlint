package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type testRepo struct {
	t    *testing.T
	root string
	home string
}

func newTestRepo(t *testing.T, initArgs ...string) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	base := t.TempDir()
	repo := &testRepo{t: t, root: filepath.Join(base, "repo"), home: filepath.Join(base, "home")}
	if err := os.MkdirAll(filepath.Join(repo.home, ".config", "git"), 0750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", repo.home)
	t.Setenv("USERPROFILE", repo.home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(repo.home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("GIT_AUTHOR_NAME", "Fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.invalid")
	if err := os.MkdirAll(repo.root, 0750); err != nil {
		t.Fatal(err)
	}
	repo.git(append([]string{"init", "--quiet"}, initArgs...)...)
	return repo
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	out, err := command.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (r *testRepo) write(name, content string) string {
	r.t.Helper()
	path := filepath.Join(r.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		r.t.Fatal(err)
	}
	return path
}

func (r *testRepo) open() *Repository {
	r.t.Helper()
	repo, err := Open(r.root)
	if err != nil {
		r.t.Fatal(err)
	}
	if repo == nil {
		r.t.Fatalf("Open(%q) = nil", r.root)
	}
	return repo
}

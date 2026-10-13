package gitrepo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTrackedFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		initArgs []string
		setup    func(fixture *testRepo)
	}{
		{"version 2", nil, func(*testRepo) {}},
		{"version 3", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--skip-worktree", "a/b/c.txt")
		}},
		{"version 4", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--index-version", "4")
		}},
		{"sha256", []string{"--object-format=sha256"}, func(*testRepo) {}},
		{"untracked cache", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--untracked-cache")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTestRepo(t, tc.initArgs...)
			for _, name := range []string{"a/b/c.txt", "a/b/d.txt", "a/e.txt", "z.txt", "ab.txt"} {
				fixture.write(name, "content\n")
			}
			fixture.git("add", "--all")
			tc.setup(fixture)
			got, err := fixture.open().TrackedFiles()
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Fields(strings.ReplaceAll(fixture.git("ls-files"), "\n", " "))
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("TrackedFiles = %q, want %q", got, want)
			}
		})
	}
}

func TestTrackedFilesKinds(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "content\n")
	if err := os.Symlink("file", filepath.Join(fixture.root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	fixture.git("add", "file", "link")
	fixture.git("update-index", "--add", "--cacheinfo", "160000,4b825dc642cb6eb9a060e54bf8d69288fbee4904,sub")
	got, err := fixture.open().TrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"file", "link"}; !slices.Equal(got, want) {
		t.Fatalf("TrackedFiles = %q, want %q", got, want)
	}
}

func TestTrackedFilesConflict(t *testing.T) {
	fixture := newTestRepo(t)
	blob := strings.TrimSpace(fixture.git("hash-object", "-w", "--stdin"))
	info := strings.Join([]string{
		"100644 " + blob + " 1\tconflict.txt",
		"100644 " + blob + " 2\tconflict.txt",
		"100644 " + blob + " 3\tconflict.txt",
		"100644 " + blob + " 0\tplain.txt",
	}, "\n") + "\n"
	path := fixture.write("index-info", info)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "git", "update-index", "--index-info")
	command.Dir = fixture.root
	command.Stdin = bytes.NewReader(data)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git update-index: %v: %s", err, out)
	}
	got, err := fixture.open().TrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"conflict.txt", "plain.txt"}; !slices.Equal(got, want) {
		t.Fatalf("TrackedFiles = %q, want %q", got, want)
	}
}

func TestTrackedFilesMissingIndex(t *testing.T) {
	fixture := newTestRepo(t)
	got, err := fixture.open().TrackedFiles()
	if err != nil || got != nil {
		t.Fatalf("TrackedFiles = %q, %v, want nil, nil", got, err)
	}
}

func TestTrackedFilesSplitIndex(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "content\n")
	fixture.git("add", "file")
	fixture.git("update-index", "--split-index")
	if got, err := fixture.open().TrackedFiles(); err == nil {
		t.Fatalf("TrackedFiles = %q, want an error for the link extension", got)
	}
}

func TestIndexPathsRejectsGarbage(t *testing.T) {
	for name, data := range map[string][]byte{
		"short":         []byte("DIRC"),
		"signature":     append([]byte("NOPE\x00\x00\x00\x02\x00\x00\x00\x00"), make([]byte, 20)...),
		"version":       append([]byte("DIRC\x00\x00\x00\x05\x00\x00\x00\x00"), make([]byte, 20)...),
		"truncated":     append([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x01"), make([]byte, 20)...),
		"bad extension": append([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x00link\x00\x00\x00\x00"), make([]byte, 20)...),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := indexPaths(data, 20); err == nil {
				t.Fatalf("indexPaths = %q, want an error", got)
			}
		})
	}
}

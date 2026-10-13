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

func TestIndexEntries(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "core.filemode", "false")
	fixture.write("plain.sh", "echo\n")
	fixture.write("exec.sh", "echo\n")
	fixture.write("intent.sh", "echo\n")
	fixture.git("add", "plain.sh", "exec.sh")
	fixture.git("update-index", "--chmod=+x", "exec.sh")
	fixture.git("add", "-N", "intent.sh")
	blob := strings.TrimSpace(fixture.git("hash-object", "-w", "--stdin"))
	path := fixture.write("index-info", "100644 "+blob+" 1\tconflict.txt\n100644 "+blob+" 2\tconflict.txt\n100755 "+blob+" 3\tconflict.txt\n")
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
	got, err := fixture.open().Index()
	if err != nil {
		t.Fatal(err)
	}
	want := []IndexEntry{
		{Path: "conflict.txt", Mode: 0o100644, Stage: 1},
		{Path: "conflict.txt", Mode: 0o100644, Stage: 2},
		{Path: "conflict.txt", Mode: 0o100755, Stage: 3},
		{Path: "exec.sh", Mode: 0o100755},
		{Path: "intent.sh", Mode: 0o100644, IntentToAdd: true},
		{Path: "plain.sh", Mode: 0o100644},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Index = %v, want %v", got, want)
	}
}

func TestTrackedFilesChecksum(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "content\n")
	fixture.git("add", "file")
	path := filepath.Join(fixture.root, ".git", "index")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := slices.Clone(data)
	corrupted[len(corrupted)-25] ^= 0x01
	if err := os.WriteFile(path, corrupted, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := fixture.open().TrackedFiles(); err == nil {
		t.Fatalf("TrackedFiles = %q, want an error for the corrupted index", got)
	}
	fixture.git("config", "index.skipHash", "true")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.git("update-index", "--force-write-index")
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rewritten[len(rewritten)-20:], make([]byte, 20)) {
		t.Skip("git kept writing the index checksum")
	}
	if got, err := fixture.open().TrackedFiles(); err != nil || !slices.Equal(got, []string{"file"}) {
		t.Fatalf("TrackedFiles = %q, %v with index.skipHash", got, err)
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
	for _, version := range []string{"2", "4"} {
		t.Run("version "+version, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.git("config", "core.splitIndex", "true")
			fixture.git("config", "splitIndex.maxPercentChange", "100")
			for _, name := range []string{"a/kept.txt", "a/removed.txt", "a/changed.txt", "b/kept.txt"} {
				fixture.write(name, "content\n")
			}
			fixture.git("add", "--all")
			fixture.git("update-index", "--index-version", version)
			fixture.git("update-index", "--split-index")
			fixture.git("rm", "--quiet", "--cached", "a/removed.txt")
			fixture.write("a/changed.txt", "changed\n")
			fixture.write("c/added.txt", "content\n")
			fixture.git("add", "a/changed.txt", "c/added.txt")
			if _, err := os.Stat(filepath.Join(fixture.root, ".git", "index")); err != nil {
				t.Fatal(err)
			}
			matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
			if err != nil || len(matches) == 0 {
				t.Fatalf("no shared index: %v", err)
			}
			got, err := fixture.open().TrackedFiles()
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"a/changed.txt", "a/kept.txt", "b/kept.txt", "c/added.txt"}; !slices.Equal(got, want) {
				t.Fatalf("TrackedFiles = %q, want %q", got, want)
			}
		})
	}
}

func TestTrackedFilesSparseIndex(t *testing.T) {
	fixture := newTestRepo(t)
	for _, name := range []string{"in/action.yml", "in/deep/file.txt", "out/action.yml", "out/deep/file.txt", "root.txt"} {
		fixture.write(name, "content\n")
	}
	fixture.git("add", "--all")
	fixture.git("commit", "--quiet", "-m", "Fixture")
	fixture.git("sparse-checkout", "set", "--cone", "--sparse-index", "in")
	if _, err := os.Stat(filepath.Join(fixture.root, "out")); !os.IsNotExist(err) {
		t.Fatalf("out/ still exists: %v", err)
	}
	sparse := fixture.git("ls-files", "--sparse")
	if !strings.Contains(sparse, "out/\n") {
		t.Fatalf("the index is not sparse: %q", sparse)
	}
	got, err := fixture.open().TrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"in/action.yml", "in/deep/file.txt", "root.txt"}; !slices.Equal(got, want) {
		t.Fatalf("TrackedFiles = %q, want %q", got, want)
	}
}

func TestTrackedFilesMissingSharedIndex(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "content\n")
	fixture.git("add", "file")
	fixture.git("update-index", "--split-index")
	matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no shared index: %v", err)
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := fixture.open().TrackedFiles(); err == nil {
		t.Fatalf("TrackedFiles = %q, want an error for the missing shared index", got)
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
			if got, err := indexEntries(data, 20, nil); err == nil {
				t.Fatalf("indexEntries = %v, want an error", got)
			}
		})
	}
}

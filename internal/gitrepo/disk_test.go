package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDiskRecordsSuccessfulReads(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	hasLink := os.Symlink("file", link) == nil
	absent := filepath.Join(dir, "absent")
	var reads []string
	d := disk{ctx: t.Context(), record: func(path string) { reads = append(reads, path) }}
	if data, err := d.readFile(file); err != nil || string(data) != "data" {
		t.Fatalf("readFile = %q, %v", data, err)
	}
	if _, err := d.readFile(absent); !missing(err) {
		t.Fatalf("readFile of an absent file = %v", err)
	}
	for _, path := range []string{absent, filepath.Join(file, "child")} {
		if data, err := d.readOptional(path); data != nil || err != nil {
			t.Fatalf("readOptional(%s) = %q, %v, want nothing", path, data, err)
		}
	}
	opened, err := d.open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.open(absent); err == nil {
		t.Fatal("open of an absent file succeeded")
	}
	if entries, err := d.readDir(dir); err != nil || len(entries) == 0 {
		t.Fatalf("readDir = %v, %v", entries, err)
	}
	if _, err := d.readDir(absent); err == nil {
		t.Fatal("readDir of an absent directory succeeded")
	}
	want := []string{file, file, dir}
	if hasLink {
		if target, err := d.readlink(link); err != nil || target != "file" {
			t.Fatalf("readlink = %q, %v", target, err)
		}
		want = append(want, link)
	}
	if _, err := d.readlink(file); err == nil {
		t.Fatal("readlink of a regular file succeeded")
	}
	if !slices.Equal(reads, want) {
		t.Fatalf("recorded %q, want %q", reads, want)
	}
}

func TestDiskCancelled(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d := disk{ctx: ctx, record: func(path string) { t.Errorf("recorded %s after cancellation", path) }}
	_, readErr := d.readFile(file)
	_, optionalErr := d.readOptional(file)
	_, openErr := d.open(file)
	_, dirErr := d.readDir(dir)
	_, linkErr := d.readlink(file)
	for name, err := range map[string]error{"readFile": readErr, "readOptional": optionalErr, "open": openErr, "readDir": dirErr, "readlink": linkErr, "wait": d.wait(time.Hour)} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s = %v, want context.Canceled", name, err)
		}
	}
}

func TestDiskWaitElapses(t *testing.T) {
	if err := testDisk(t).wait(time.Millisecond); err != nil {
		t.Fatalf("wait = %v", err)
	}
}

func TestPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if permissionDenied(file) || permissionDenied(filepath.Join(dir, "absent")) {
		t.Fatal("permissionDenied reported a readable or absent file")
	}
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot make a file unreadable on Windows")
	}
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
	if data, err := os.ReadFile(file); err == nil {
		t.Skipf("the file stays readable, as for root: %q", data)
	}
	if !permissionDenied(file) {
		t.Fatal("permissionDenied missed an unreadable file")
	}
}

func TestOpenCancelled(t *testing.T) {
	fixture := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if repo, err := Open(ctx, fixture.root); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open = %+v, %v, want context.Canceled", repo, err)
	}
	repo := fixture.open()
	if branch, err := repo.branch(disk{ctx: ctx, record: func(string) {}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("branch = %q, %v, want context.Canceled", branch, err)
	}
	if _, err := repo.config(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("config = %v, want context.Canceled", err)
	}
	fixture.write(".gitignore", "ignored/\n")
	if ignores, err := repo.Ignores(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ignores = %+v, %v, want context.Canceled", ignores, err)
	}
	if entries, err := repo.Index(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Index = %v, %v, want context.Canceled", entries, err)
	}
}

func TestInflaterHeader(t *testing.T) {
	for name, tc := range map[string]struct {
		stream string
		want   string
		err    string
	}{
		"short":        {stream: "tree 3\x00abc", want: "tree 3"},
		"at the limit": {stream: strings.Repeat("t", looseHeaderMaximum-1) + "\x00", want: strings.Repeat("t", looseHeaderMaximum-1)},
		"too long":     {stream: strings.Repeat("t", looseHeaderMaximum) + "\x00", err: "longer than"},
		"unterminated": {stream: "tree 3", err: "unterminated"},
	} {
		t.Run(name, func(t *testing.T) {
			compressed := deflate(tc.stream)
			z, err := newInflater(strings.NewReader(string(compressed)), uint64(len(compressed)))
			if err != nil {
				t.Fatal(err)
			}
			header, err := z.header()
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("header = %q, %v, want %q", header, err, tc.err)
				}
				return
			}
			if err != nil || string(header) != tc.want {
				t.Fatalf("header = %q, %v, want %q", header, err, tc.want)
			}
		})
	}
}

func TestInflaterExact(t *testing.T) {
	inflater := func(compressed []byte) *inflater {
		t.Helper()
		z, err := newInflater(strings.NewReader(string(compressed)), uint64(len(compressed)))
		if err != nil {
			t.Fatal(err)
		}
		return z
	}
	compressed := deflate("abc")
	if data, err := inflater(compressed).exact(3); err != nil || string(data) != "abc" {
		t.Fatalf("exact = %q, %v", data, err)
	}
	if _, err := inflater(compressed).exact(1 << 40); err == nil || !strings.Contains(err.Error(), "claim") {
		t.Fatalf("exact for an impossible size = %v", err)
	}
	if _, err := inflater(compressed).exact(2); err == nil || !strings.Contains(err.Error(), "does not end") {
		t.Fatalf("exact short of the stream = %v", err)
	}
	if _, err := inflater(compressed).exact(4); err == nil {
		t.Fatal("exact past the stream succeeded")
	}
	z := inflater(compressed)
	if _, err := z.exact(3); err != nil {
		t.Fatal(err)
	}
	if err := z.finished(); err != nil {
		t.Fatalf("finished = %v", err)
	}
	z = inflater(append(compressed, 'x'))
	if _, err := z.exact(3); err != nil {
		t.Fatal(err)
	}
	if err := z.finished(); err == nil {
		t.Fatal("finished accepted bytes after the stream")
	}
	if _, err := newInflater(strings.NewReader("not zlib"), 8); err == nil {
		t.Fatal("newInflater accepted a stream without a zlib header")
	}
}

func TestReadFilesObjectStore(t *testing.T) {
	assertRecorded := func(t *testing.T, repo *Repository, want ...string) {
		t.Helper()
		if _, err := repo.ExpandedIndex(t.Context()); err != nil {
			t.Fatal(err)
		}
		reads := repo.ReadFiles()
		for _, path := range want {
			if !slices.Contains(reads, path) {
				t.Errorf("ReadFiles %q lacks %q", reads, path)
			}
		}
	}
	t.Run("loose", func(t *testing.T) {
		fixture := sparseFixture(t)
		repo := fixture.open()
		oid := fixture.treeOID("out/bulk")
		assertRecorded(t, repo, filepath.Join(repo.CommonDir, "objects", "pack"), filepath.Join(repo.CommonDir, "objects", oid[:2], oid[2:]))
	})
	t.Run("packed", func(t *testing.T) {
		fixture := sparseFixture(t)
		fixture.git("repack", "-a", "-d", "-q")
		packs, err := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.pack"))
		if err != nil || len(packs) != 1 {
			t.Fatalf("packs %q: %v, want one", packs, err)
		}
		repo := fixture.open()
		pack := filepath.Join(repo.CommonDir, "objects", "pack", filepath.Base(packs[0]))
		assertRecorded(t, repo, filepath.Dir(pack), pack, strings.TrimSuffix(pack, ".pack")+".idx")
	})
	t.Run("multi-pack-index", func(t *testing.T) {
		fixture := sparseFixture(t)
		fixture.git("repack", "-a", "-d", "-q")
		fixture.git("multi-pack-index", "write")
		repo := fixture.open()
		assertRecorded(t, repo, filepath.Join(repo.CommonDir, "objects", "pack", "multi-pack-index"))
	})
}

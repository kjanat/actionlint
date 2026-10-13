package gitrepo

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func sparseFixture(t *testing.T, initArgs ...string) *testRepo {
	t.Helper()
	fixture := newTestRepo(t, initArgs...)
	fixture.writeFiles("in/action.yml", "in/deep/file.txt", "out/action.yml", "out/scripts/run.sh", "out/a-b.txt", "out/a/c.txt", "out/deep/file.txt", "out/deep/more/x.txt", "out/deep/zz.txt", "root.txt")
	for i := range 40 {
		fixture.write(fmt.Sprintf("out/bulk/f%02d.txt", i), strings.Repeat(fmt.Sprintf("line %d\n", i), 50))
	}
	if err := os.Chmod(filepath.Join(fixture.root, "out", "scripts", "run.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.git("add", "--all")
	fixture.git("update-index", "--chmod=+x", "out/scripts/run.sh")
	if err := os.Symlink("run.sh", filepath.Join(fixture.root, "out", "scripts", "link")); err == nil {
		fixture.git("add", "out/scripts/link")
	}
	hexLength := 40
	if strings.TrimSpace(fixture.git("rev-parse", "--show-object-format")) == "sha256" {
		hexLength = 64
	}
	fixture.git("update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("1", hexLength)+",out/sub")
	fixture.git("commit", "--quiet", "--no-gpg-sign", "-m", "first")
	fixture.git("rm", "--quiet", "out/bulk/f00.txt", "out/bulk/f01.txt")
	fixture.write("out/deep/file.txt", "changed\n")
	fixture.git("add", "out/deep/file.txt")
	fixture.git("commit", "--quiet", "--no-gpg-sign", "-m", "second")
	fixture.git("sparse-checkout", "set", "--cone", "--sparse-index", "in")
	if sparse := fixture.git("ls-files", "--sparse"); !strings.Contains(sparse, "out/scripts/\n") {
		t.Fatalf("the index is not sparse: %q", sparse)
	}
	return fixture
}

func (r *testRepo) treeOID(path string) string {
	r.t.Helper()
	return strings.TrimSpace(r.git("rev-parse", "HEAD:"+path))
}

func (r *testRepo) looseObjects() []string {
	r.t.Helper()
	var objects []string
	dirs, err := filepath.Glob(filepath.Join(r.root, ".git", "objects", "??"))
	if err != nil {
		r.t.Fatal(err)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, entry := range entries {
			objects = append(objects, filepath.Base(dir)+entry.Name())
		}
	}
	return objects
}

func (r *testRepo) packedKind(oid string) objectType {
	r.t.Helper()
	repo := r.open()
	settings, err := repo.config()
	if err != nil {
		r.t.Fatal(err)
	}
	store, err := repo.openObjectStore(settings)
	if err != nil {
		r.t.Fatal(err)
	}
	defer store.close()
	raw, err := hex.DecodeString(oid)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, pack := range store.sources[0].packs {
		if err := pack.open(repo.hashLength); err != nil {
			r.t.Fatal(err)
		}
		position, ok := pack.index.find(raw)
		if !ok {
			continue
		}
		offset, err := pack.index.offset(position)
		if err != nil {
			r.t.Fatal(err)
		}
		header, err := pack.window(offset)
		if err != nil {
			r.t.Fatal(err)
		}
		kind, _, _, err := packObjectHeader(header)
		if err != nil {
			r.t.Fatal(err)
		}
		return kind
	}
	r.t.Fatalf("%s is not packed", oid)
	return 0
}

func TestExpandedIndexLooseObjects(t *testing.T) {
	fixture := sparseFixture(t)
	if loose := fixture.looseObjects(); !slices.Contains(loose, fixture.treeOID("out/bulk")) {
		t.Fatalf("the out/ tree is not loose: %v", loose)
	}
	expanded := fixture.compareIndex()
	assertExpandedScripts(t, expanded)
}

func assertExpandedScripts(t *testing.T, entries []IndexEntry) {
	t.Helper()
	modes := make(map[string]IndexEntry)
	for _, entry := range entries {
		modes[entry.Path] = entry
	}
	if entry := modes["out/scripts/run.sh"]; entry.Mode != 0o100755 || !entry.SkipWorktree {
		t.Fatalf("out/scripts/run.sh = %+v, want an executable skip-worktree entry", entry)
	}
	if entry, ok := modes["out/scripts/link"]; ok && entry.Mode != 0o120000 {
		t.Fatalf("out/scripts/link = %+v, want a symlink", entry)
	}
}

func TestExpandedIndexPackedObjects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		initArgs []string
		pack     func(fixture *testRepo)
		check    func(t *testing.T, fixture *testRepo)
	}{
		{"gc", nil, func(fixture *testRepo) {
			fixture.git("gc", "--quiet")
		}, nil},
		{"offset deltas", nil, func(fixture *testRepo) {
			fixture.git("repack", "-a", "-d", "-f", "-q", "--window=250", "--depth=50")
			fixture.git("prune-packed")
		}, func(t *testing.T, fixture *testRepo) {
			if kind := fixture.packedKind(fixture.treeOID("out/bulk")); kind != objectOffsetDelta {
				t.Fatalf("the out/ tree is packed as type %d, want an offset delta", kind)
			}
		}},
		{"reference deltas", nil, func(fixture *testRepo) {
			fixture.git("-c", "repack.useDeltaBaseOffset=false", "repack", "-a", "-d", "-f", "-q", "--window=250", "--depth=50")
			fixture.git("prune-packed")
		}, func(t *testing.T, fixture *testRepo) {
			if kind := fixture.packedKind(fixture.treeOID("out/bulk")); kind != objectRefDelta {
				t.Fatalf("the out/ tree is packed as type %d, want a reference delta", kind)
			}
		}},
		{"sha256 deltas", []string{"--object-format=sha256"}, func(fixture *testRepo) {
			fixture.git("repack", "-a", "-d", "-f", "-q", "--window=250", "--depth=50")
			fixture.git("prune-packed")
		}, func(t *testing.T, fixture *testRepo) {
			if kind := fixture.packedKind(fixture.treeOID("out/bulk")); kind != objectOffsetDelta {
				t.Fatalf("the out/ tree is packed as type %d, want an offset delta", kind)
			}
		}},
		{"sha256 multi-pack-index", []string{"--object-format=sha256"}, func(fixture *testRepo) {
			fixture.twoPacks(func() {})
			fixture.git("multi-pack-index", "write")
		}, func(t *testing.T, fixture *testRepo) {
			assertMultiPackIndex(t, fixture, 2)
		}},
		{"index version 1", nil, func(fixture *testRepo) {
			fixture.git("-c", "pack.indexVersion=1", "repack", "-a", "-d", "-q")
			fixture.git("prune-packed")
		}, func(t *testing.T, fixture *testRepo) {
			matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.idx"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("pack indexes %v: %v", matches, err)
			}
			data, err := os.ReadFile(matches[0])
			if err != nil || bytes.HasPrefix(data, packIndexSignature) {
				t.Fatalf("the pack index is not version 1: %v", err)
			}
		}},
		{"two packs", nil, func(fixture *testRepo) {
			fixture.twoPacks(func() {})
		}, func(t *testing.T, fixture *testRepo) {
			assertMultiPackIndex(t, fixture, 0)
		}},
		{"multi-pack-index", nil, func(fixture *testRepo) {
			fixture.twoPacks(func() {})
			fixture.git("multi-pack-index", "write")
		}, func(t *testing.T, fixture *testRepo) {
			assertMultiPackIndex(t, fixture, 2)
		}},
		{"incremental multi-pack-index", nil, func(fixture *testRepo) {
			fixture.twoPacks(func() { fixture.git("multi-pack-index", "write", "--incremental") })
			fixture.git("multi-pack-index", "write", "--incremental")
		}, func(t *testing.T, fixture *testRepo) {
			if _, err := os.Stat(filepath.Join(fixture.root, ".git", "objects", "pack", "multi-pack-index.d", "multi-pack-index-chain")); err != nil {
				t.Fatal(err)
			}
			assertMultiPackIndex(t, fixture, 2)
		}},
		{"multi-pack-index disabled", nil, func(fixture *testRepo) {
			fixture.twoPacks(func() {})
			fixture.git("multi-pack-index", "write")
			fixture.git("config", "core.multiPackIndex", "false")
		}, func(t *testing.T, fixture *testRepo) {
			assertMultiPackIndex(t, fixture, 0)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := sparseFixture(t, tc.initArgs...)
			tc.pack(fixture)
			if loose := fixture.looseObjects(); slices.Contains(loose, fixture.treeOID("out/bulk")) {
				t.Fatalf("the out/ tree is still loose")
			}
			if tc.check != nil {
				tc.check(t, fixture)
			}
			assertExpandedScripts(t, fixture.compareIndex())
		})
	}
}

func (r *testRepo) twoPacks(between func()) {
	r.t.Helper()
	r.gitInput(r.git("rev-list", "--objects", "HEAD~1"), "pack-objects", "-q", filepath.Join(r.root, ".git", "objects", "pack", "pack"))
	between()
	r.git("repack", "-d", "-q")
	r.git("prune-packed")
	if packs, err := filepath.Glob(filepath.Join(r.root, ".git", "objects", "pack", "*.pack")); err != nil || len(packs) != 2 {
		r.t.Fatalf("packs %v: %v, want two", packs, err)
	}
}

func assertMultiPackIndex(t *testing.T, fixture *testRepo, packs int) {
	t.Helper()
	repo := fixture.open()
	settings, err := repo.config()
	if err != nil {
		t.Fatal(err)
	}
	store, err := repo.openObjectStore(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	source := store.sources[0]
	got := 0
	for layer := source.midx; layer != nil; layer = layer.base {
		got += len(layer.packs)
	}
	if got != packs || packs > 0 && len(source.packs) != 0 {
		t.Fatalf("multi-pack-index covers %d packs with %d separate packs, want %d covered", got, len(source.packs), packs)
	}
}

func TestExpandedIndexAlternates(t *testing.T) {
	source := sparseFixture(t)
	source.git("repack", "-a", "-d", "-q")
	source.git("prune-packed")
	clone := &testRepo{t: t, root: filepath.Join(filepath.Dir(source.root), "clone"), home: source.home}
	source.git("clone", "--quiet", "--shared", "--no-checkout", source.root, clone.root)
	nested := &testRepo{t: t, root: filepath.Join(filepath.Dir(source.root), "nested"), home: source.home}
	clone.git("clone", "--quiet", "--shared", "--no-checkout", clone.root, nested.root)
	for _, fixture := range []*testRepo{clone, nested} {
		fixture.git("sparse-checkout", "set", "--cone", "--sparse-index", "in")
		fixture.git("checkout", "--quiet", "HEAD")
		if packs, _ := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.pack")); len(packs) != 0 || len(fixture.looseObjects()) != 0 {
			t.Fatalf("%s has its own objects: %v", fixture.root, packs)
		}
		assertExpandedScripts(t, fixture.compareIndex())
	}
}

func TestExpandedIndexMissingTree(t *testing.T) {
	fixture := sparseFixture(t)
	oid := fixture.treeOID("out/deep/more")
	if err := os.Remove(filepath.Join(fixture.root, ".git", "objects", oid[:2], oid[2:])); err != nil {
		t.Fatal(err)
	}
	assertStoppedInDeep(t, fixture.compareIndex())
}

func assertStoppedInDeep(t *testing.T, entries []IndexEntry) {
	t.Helper()
	var deep []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "out/deep/") {
			deep = append(deep, entry.Path)
		}
	}
	if !slices.Equal(deep, []string{"out/deep/file.txt"}) {
		t.Fatalf("out/deep/ expanded to %q, want the walk to stop after out/deep/file.txt", deep)
	}
}

func TestExpandedIndexGitlinkInTree(t *testing.T) {
	fixture := sparseFixture(t)
	oid, err := hex.DecodeString(fixture.treeOID("out"))
	if err != nil {
		t.Fatal(err)
	}
	data := encodeIndex(3, []rawIndexEntry{{mode: 0o040000, flags: indexExtendedFlag | 4, extended: indexSkipWorktreeFlag, name: []byte("out/"), oid: oid}})
	if err := os.WriteFile(filepath.Join(fixture.root, ".git", "index"), data, 0600); err != nil {
		t.Fatal(err)
	}
	expanded := fixture.compareIndex()
	if !slices.ContainsFunc(expanded, func(e IndexEntry) bool { return e.Path == "out/sub" && e.Mode == 0o160000 && e.SkipWorktree }) {
		t.Fatalf("ExpandedIndex = %v, want the out/sub gitlink", expanded)
	}
	assertExpandedScripts(t, expanded)
}

func TestExpandedIndexCorruptTree(t *testing.T) {
	for name, content := range map[string][]byte{
		"zlib":             []byte("not zlib"),
		"header":           deflate("tree x\x00"),
		"short tree":       deflate("tree 3\x00abc"),
		"mode":             deflate("tree 29\x0010x644 a\x00" + strings.Repeat("\x01", 20)),
		"type":             deflate("blob 3\x00abc"),
		"unterminated":     deflate("tree 3"),
		"no size":          deflate("tree\x00"),
		"unknown type":     deflate("tre 3\x00abc"),
		"size mismatch":    deflate("tree 5\x00abc"),
		"dot git":          deflate("tree 32\x00100644 .git\x00" + strings.Repeat("\x01", 20)),
		"truncated stream": deflate("tree 3\x00abc")[:10],
		"bad mode":         deflate("tree 29\x00100648 a\x00" + strings.Repeat("\x01", 20)),
	} {
		t.Run(name, func(t *testing.T) {
			fixture := sparseFixture(t)
			path := filepath.Join(fixture.root, ".git", "objects", fixture.treeOID("out/bulk")[:2], fixture.treeOID("out/bulk")[2:])
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			out, failed := fixture.gitFails("ls-files")
			got, err := fixture.open().ExpandedIndex(t.Context())
			if failed != (err != nil) {
				t.Fatalf("git ls-files failed = %v (%s), ExpandedIndex = %v, %v", failed, out, got, err)
			}
			if !failed {
				fixture.compareIndex()
			}
		})
	}
}

func deflate(content string) []byte {
	var out bytes.Buffer
	writer := zlib.NewWriter(&out)
	_, _ = writer.Write([]byte(content))
	_ = writer.Close()
	return out.Bytes()
}

func TestExpandedIndexBadConfig(t *testing.T) {
	for name, damage := range map[string]func(fixture *testRepo){
		"protect ntfs": func(fixture *testRepo) { fixture.git("config", "core.protectNTFS", "maybe") },
		"multi-pack-index": func(fixture *testRepo) {
			fixture.git("config", "core.multiPackIndex", "maybe")
		},
		"no system": func(fixture *testRepo) { fixture.t.Setenv("GIT_CONFIG_NOSYSTEM", "maybe") },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := sparseFixture(t)
			repo := fixture.open()
			damage(fixture)
			if out, failed := fixture.gitFails("ls-files"); !failed {
				t.Fatalf("git ls-files accepted the configuration: %s", out)
			}
			if got, err := repo.ExpandedIndex(t.Context()); err == nil {
				t.Fatalf("ExpandedIndex = %v, want an error", got)
			}
		})
	}
}

func TestExpandedIndexTreeDepth(t *testing.T) {
	fixture := sparseFixture(t)
	fixture.git("config", "core.maxTreeDepth", "0")
	assertStoppedInDeep(t, fixture.compareIndex())
	fixture.git("config", "core.maxTreeDepth", "deep")
	if out, failed := fixture.gitFails("ls-files"); !failed {
		t.Fatalf("git ls-files accepted core.maxTreeDepth=deep: %s", out)
	}
	if got, err := fixture.open().ExpandedIndex(t.Context()); err == nil {
		t.Fatalf("ExpandedIndex = %v, want an error for a bad core.maxTreeDepth", got)
	}
}

func TestApplyDelta(t *testing.T) {
	base := []byte("0123456789")
	for name, tc := range map[string]struct {
		delta []byte
		want  string
	}{
		"copy and insert":  {[]byte{10, 7, 0x91, 2, 3, 4, 'a', 'b', 'c', 'd'}, "234abcd"},
		"default size":     {[]byte{10, 0, 0x80}, ""},
		"zero opcode":      {[]byte{10, 1, 0, 1}, ""},
		"base size":        {[]byte{9, 1, 1, 'a'}, ""},
		"copy past base":   {[]byte{10, 3, 0x91, 9, 3}, ""},
		"insert too much":  {[]byte{10, 1, 2, 'a', 'b'}, ""},
		"short result":     {[]byte{10, 5, 1, 'a'}, ""},
		"truncated copy":   {[]byte{10, 3, 0x91, 9}, ""},
		"too short":        {[]byte{10, 0}, ""},
		"truncated source": {[]byte{0x80, 0x80, 0x80, 0x80}, ""},
		"truncated result": {[]byte{10, 0x80, 0x80, 0x80}, ""},
		"overflow":         {[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := applyDelta(base, tc.delta)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("applyDelta = %q, want an error", got)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("applyDelta = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestApplyDeltaDefaultCopySize(t *testing.T) {
	base := bytes.Repeat([]byte("x"), 0x10000)
	got, err := applyDelta(base, []byte{0x80, 0x80, 0x04, 0x80, 0x80, 0x04, 0x80})
	if err != nil || !bytes.Equal(got, base) {
		t.Fatalf("applyDelta = %d bytes, %v, want a 0x10000-byte copy", len(got), err)
	}
}

func TestSearchFanout(t *testing.T) {
	names := [][]byte{{0, 1}, {0, 2}, {0, 3}, {1, 0}}
	fanout := func(i int) uint32 {
		if i == 0 {
			return 3
		}
		return 4
	}
	name := func(n uint32) []byte { return names[n] }
	for want, oid := range names {
		if got, ok := searchFanout(oid, fanout, name); !ok || got != uint32(want) {
			t.Errorf("searchFanout(%x) = %d, %v, want %d", oid, got, ok, want)
		}
	}
	if got, ok := searchFanout([]byte{0, 4}, fanout, name); ok {
		t.Errorf("searchFanout found a missing object at %d", got)
	}
}

func TestRealPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if got := realPath(missing); got != missing {
		t.Fatalf("realPath(%q) = %q", missing, got)
	}
}

func TestPackObjectHeader(t *testing.T) {
	kind, size, used, err := packObjectHeader([]byte{0x9f, 0x01})
	if err != nil || kind != objectCommit || size != 31 || used != 2 {
		t.Fatalf("packObjectHeader = %d, %d, %d, %v", kind, size, used, err)
	}
	for name, header := range map[string][]byte{
		"empty":     nil,
		"truncated": {0x9f},
		"overflow":  append(bytes.Repeat([]byte{0xff}, 10), 0x7f),
	} {
		if _, _, _, err := packObjectHeader(header); err == nil {
			t.Errorf("%s: packObjectHeader accepted %x", name, header)
		}
	}
}

func TestParseTree(t *testing.T) {
	oid := strings.Repeat("\x01", 20)
	entries, err := parseTree([]byte("100664 a\x00"+oid+"40000 d\x00"+oid+"120000 l\x00"+oid+"160000 s\x00"+oid+"100755 x\x00"+oid+"1100644 w\x00"+oid), 20)
	if err != nil {
		t.Fatal(err)
	}
	var modes []string
	for _, entry := range entries {
		modes = append(modes, string(entry.name)+"="+strconv.FormatUint(uint64(entry.mode), 8))
	}
	if want := []string{"a=100644", "d=40000", "l=120000", "s=160000", "x=100755", "w=100644"}; !slices.Equal(modes, want) {
		t.Fatalf("modes = %q, want %q", modes, want)
	}
	for name, data := range map[string]string{
		"short":          "1 a\x00",
		"leading space":  " 100644 a\x00" + oid,
		"non-octal mode": "100648 a\x00" + oid,
		"empty name":     "100644 \x00" + oid,
	} {
		if got, err := parseTree([]byte(data), 20); err == nil {
			t.Errorf("%s: parseTree = %v, want an error", name, got)
		}
	}
}

func TestReadAlternates(t *testing.T) {
	root := t.TempDir()
	objects := filepath.Join(root, "repo", "objects")
	for _, dir := range []string{objects, filepath.Join(root, "relative"), filepath.Join(root, "quoted dir"), filepath.Join(root, "absolute")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(objects, "info"), 0750); err != nil {
		t.Fatal(err)
	}
	content := "# comment\n../../relative\n\n\"" + filepath.ToSlash(root) + "/quoted\\040dir\"\n" + filepath.Join(root, "absolute") + "/\n" + filepath.Join(root, "missing") + "\n"
	if err := os.WriteFile(filepath.Join(objects, "info", "alternates"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	resolved := realPath(root)
	want := []string{filepath.Join(resolved, "relative"), filepath.Join(resolved, "quoted dir"), filepath.Join(resolved, "absolute")}
	var reads []string
	if got := readAlternates(objects, func(path string) { reads = append(reads, path) }); !slices.Equal(got, want) {
		t.Fatalf("readAlternates = %q, want %q", got, want)
	}
	if want := []string{filepath.Join(objects, "info", "alternates")}; !slices.Equal(reads, want) {
		t.Fatalf("recorded %q, want %q", reads, want)
	}
	reads = nil
	if got := readAlternates(filepath.Join(root, "absent"), func(path string) { reads = append(reads, path) }); got != nil || reads != nil {
		t.Fatalf("readAlternates without a file = %q, recorded %q", got, reads)
	}
}

func TestReadFilesAlternates(t *testing.T) {
	fixture := sparseFixture(t)
	objects := filepath.Join(fixture.root, ".git", "objects")
	moved := filepath.Join(filepath.Dir(fixture.root), "elsewhere", "objects")
	if err := os.MkdirAll(filepath.Dir(moved), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(objects, moved); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(filepath.Dir(fixture.root), "second", "objects")
	if err := os.MkdirAll(filepath.Join(second, "pack"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(objects, "pack"), filepath.Join(moved, "info")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	fixture.write(".git/objects/info/alternates", moved+"\n")
	if err := os.WriteFile(filepath.Join(moved, "info", "alternates"), []byte(second+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := fixture.open()
	assertExpandedScripts(t, fixture.compareIndex())
	if _, err := repo.ExpandedIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	reads := repo.ReadFiles()
	for _, want := range []string{filepath.Join(repo.CommonDir, "objects", "info", "alternates"), filepath.Join(realPath(moved), "info", "alternates")} {
		if !slices.Contains(reads, want) {
			t.Errorf("ReadFiles %q lacks %q", reads, want)
		}
	}
}

func TestUnquoteC(t *testing.T) {
	for input, want := range map[string]string{`"a\tb"x`: "a\tb", `"\101\\\""`: `A\"`} {
		if got, _, ok := unquoteC(input); !ok || got != want {
			t.Errorf("unquoteC(%q) = %q, %v, want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{`plain`, `"open`, `"\q"`, `"\4`, `"a\`} {
		if got, _, ok := unquoteC(input); ok {
			t.Errorf("unquoteC(%q) = %q, want a failure", input, got)
		}
	}
}

func TestLoadPackIndexRejects(t *testing.T) {
	dir := t.TempDir()
	fanout := make([]byte, 4*256)
	fanout[3] = 2
	for name, data := range map[string][]byte{
		"short":          make([]byte, 100),
		"version 3":      slices.Concat(packIndexSignature, []byte{0, 0, 0, 3}, make([]byte, 4*256+40)),
		"non-monotonic":  slices.Concat(fanout, make([]byte, 40)),
		"v1 size":        slices.Concat(make([]byte, 4*256), make([]byte, 41)),
		"v2 size":        slices.Concat(packIndexSignature, []byte{0, 0, 0, 2}, make([]byte, 4*256), make([]byte, 41)),
		"v2 large table": slices.Concat(packIndexSignature, []byte{0, 0, 0, 2}, make([]byte, 4*256), make([]byte, 48)),
	} {
		path := filepath.Join(dir, name+".idx")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := loadPackIndex(path, 20); err == nil {
			t.Errorf("%s: loadPackIndex = %+v, want an error", name, got)
		}
	}
}

func TestWalkTreeCancelled(t *testing.T) {
	fixture := sparseFixture(t)
	repo := fixture.open()
	settings, err := repo.config()
	if err != nil {
		t.Fatal(err)
	}
	store, err := repo.openObjectStore(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	oid, err := hex.DecodeString(fixture.treeOID("out"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = store.walkTree(ctx, oid, []byte("out/"), 0, defaultMaxTreeDepth, pathRules{}, func(indexEntry) { t.Fatal("walked a cancelled tree") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("walkTree = %v, want context.Canceled", err)
	}
}

func TestExpandedIndexEnvironmentAlternates(t *testing.T) {
	fixture := sparseFixture(t)
	objects := filepath.Join(fixture.root, ".git", "objects")
	moved := filepath.Join(filepath.Dir(fixture.root), "elsewhere", "objects")
	if err := os.MkdirAll(filepath.Dir(moved), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(objects, moved); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(objects, "info"), filepath.Join(objects, "pack")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", "# comment"+string(os.PathListSeparator)+moved)
	assertExpandedScripts(t, fixture.compareIndex())
	t.Chdir(filepath.Join(fixture.root, "in"))
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join("..", "elsewhere", "objects"))
	assertExpandedScripts(t, fixture.compareIndex())
}

func TestParseAlternates(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"relative", "quoted dir", "absolute"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0750); err != nil {
			t.Fatal(err)
		}
	}
	resolved := realPath(root)
	want := []string{filepath.Join(resolved, "relative"), filepath.Join(resolved, "quoted dir"), filepath.Join(resolved, "absolute")}
	sep := string(os.PathListSeparator)
	data := "# comment" + sep + "relative" + sep + "\"" + filepath.ToSlash(root) + "/quoted\\040dir\"" + sep + filepath.Join(root, "absolute") + sep + filepath.Join(root, "missing")
	if got := parseAlternates(data, os.PathListSeparator, resolved); !slices.Equal(got, want) {
		t.Fatalf("parseAlternates = %q, want %q", got, want)
	}
}

func partialCloneFixture(t *testing.T) (*testRepo, string) {
	t.Helper()
	source := newTestRepo(t, "--initial-branch=main")
	source.writeFiles("in/action.yml", "out/action.yml", "out/deep/x.txt", "root.txt")
	source.git("add", "--all")
	source.git("commit", "--quiet", "--no-gpg-sign", "-m", "base")
	source.git("config", "uploadpack.allowFilter", "true")
	clone := &testRepo{t: t, root: filepath.Join(filepath.Dir(source.root), "clone"), home: source.home}
	source.git("clone", "--quiet", "--no-local", "--no-checkout", "--filter=tree:0", source.root, clone.root)
	oid := source.treeOID("out")
	raw, err := hex.DecodeString(oid)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"core.sparseCheckout": "true", "core.sparseCheckoutCone": "true", "index.sparse": "true"} {
		clone.git("config", key, value)
	}
	clone.write(".git/info/sparse-checkout", "/*\n!/*/\n/in/\n")
	data := encodeIndex(3, []rawIndexEntry{{mode: 0o040000, flags: indexExtendedFlag | 4, extended: indexSkipWorktreeFlag, name: []byte("out/"), oid: raw}})
	if err := os.WriteFile(filepath.Join(clone.root, ".git", "index"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return clone, oid
}

func (r *testRepo) objectMissing(oid string) bool {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", "cat-file", "-e", oid)
	command.Dir = r.root
	command.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	return command.Run() != nil
}

func TestExpandedIndexPartialClone(t *testing.T) {
	for name, tc := range map[string]struct {
		keep      []string
		extension string
		lazy      bool
	}{
		"as cloned": {keep: []string{"promisor", "partialclonefilter"}, lazy: true},
		"promisor":  {keep: []string{"promisor"}, lazy: true},
		"filter":    {keep: []string{"partialclonefilter"}, lazy: true},
		"extension": {extension: "origin", lazy: true},
		"neither":   {},
		"slash":     {extension: "/origin"},
	} {
		t.Run(name, func(t *testing.T) {
			clone, oid := partialCloneFixture(t)
			for _, variable := range []string{"promisor", "partialclonefilter"} {
				if !slices.Contains(tc.keep, variable) {
					clone.git("config", "--unset", "remote.origin."+variable)
				}
			}
			if tc.extension != "" {
				clone.git("config", "extensions.partialClone", tc.extension)
			}
			if !clone.objectMissing(oid) {
				t.Fatal("the clone already has the out/ tree")
			}
			entries, err := clone.open().ExpandedIndex(t.Context())
			if tc.lazy && (err == nil || !strings.Contains(err.Error(), "promisor remote")) {
				t.Fatalf("ExpandedIndex = %v, %v; want the promisor remote error", entries, err)
			}
			if !tc.lazy && (err != nil || len(entries) != 0) {
				t.Fatalf("ExpandedIndex = %v, %v; want no entries", entries, err)
			}
			t.Setenv("GIT_NO_LAZY_FETCH", "1")
			if entries, err := clone.open().ExpandedIndex(t.Context()); err != nil || len(entries) != 0 {
				t.Fatalf("ExpandedIndex with GIT_NO_LAZY_FETCH = %v, %v; want no entries", entries, err)
			}
			if _, _, err := gitIn(t, clone.root, "cat-file", "-t", oid); err == nil || !clone.objectMissing(oid) {
				t.Fatal("git fetched the out/ tree with GIT_NO_LAZY_FETCH")
			}
			t.Setenv("GIT_NO_LAZY_FETCH", "0")
			_, _, err = gitIn(t, clone.root, "cat-file", "-t", oid)
			if fetched := err == nil && !clone.objectMissing(oid); fetched != tc.lazy {
				t.Fatalf("git fetched the out/ tree: %v, want %v", fetched, tc.lazy)
			}
			if tc.lazy {
				clone.compareIndex()
			}
		})
	}
}

func TestFetchesMissingObjects(t *testing.T) {
	for name, tc := range map[string]struct {
		env      string
		partial  string
		settings config
		want     bool
		err      string
	}{
		"nothing":            {settings: config{}},
		"extension":          {partial: "origin", settings: config{}, want: true},
		"remote":             {settings: config{"remote.origin.promisor": {valueOf("true")}}, want: true},
		"bare remote":        {settings: config{"remote.origin.promisor": {{bare: true}}}, want: true},
		"later false":        {settings: config{"remote.origin.promisor": {valueOf("true"), valueOf("false")}}, want: true},
		"remote false":       {settings: config{"remote.origin.promisor": {valueOf("false")}}},
		"no subsection":      {settings: config{"remote.promisor": {valueOf("yes")}}, want: true},
		"other key":          {settings: config{"remote.origin.promisorx": {valueOf("true")}}},
		"filter":             {settings: config{"remote.origin.partialclonefilter": {valueOf("tree:0")}}, want: true},
		"empty filter":       {settings: config{"remote.origin.partialclonefilter": {valueOf("")}}, want: true},
		"bare filter":        {settings: config{"remote.origin.partialclonefilter": {{bare: true}}}, err: "missing value"},
		"dotted name":        {settings: config{"remote.a.b.partialclonefilter": {valueOf("blob:none")}}, want: true},
		"slash promisor":     {settings: config{"remote./x.promisor": {valueOf("true")}}},
		"slash bad promisor": {settings: config{"remote./x.promisor": {valueOf("maybe")}}, err: "bad boolean config value"},
		"slash bare filter":  {settings: config{"remote./x.partialclonefilter": {{bare: true}}}},
		"slash extension":    {partial: "/origin", settings: config{}},
		"bad remote":         {settings: config{"remote.origin.promisor": {valueOf("maybe")}}, err: "bad boolean config value"},
		"bad remote too":     {partial: "origin", settings: config{"remote.origin.promisor": {valueOf("maybe")}}, err: "bad boolean config value"},
		"disabled":           {env: "1", partial: "origin", settings: config{"remote.origin.promisor": {valueOf("true")}}},
		"enabled explicitly": {env: "false", partial: "origin", settings: config{}, want: true},
		"empty env":          {env: "", partial: "origin", settings: config{}, want: true},
		"bad env":            {env: "maybe", partial: "origin", settings: config{}, err: "GIT_NO_LAZY_FETCH"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GIT_NO_LAZY_FETCH", tc.env)
			if tc.env == "" && name != "empty env" {
				if err := os.Unsetenv("GIT_NO_LAZY_FETCH"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := (&Repository{partialClone: tc.partial}).fetchesMissingObjects(tc.settings)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("fetchesMissingObjects = %v, %v; want error %q", got, err, tc.err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("fetchesMissingObjects = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestParseAlternatesRootedEntries(t *testing.T) {
	root := t.TempDir()
	resolved := realPath(root)
	rooted := `\rooted`
	if runtime.GOOS == "windows" {
		rooted = resolved[len(filepath.VolumeName(resolved)):] + `\rooted`
		if err := os.Mkdir(filepath.Join(resolved, "rooted"), 0o750); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Mkdir(filepath.Join(root, rooted), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(resolved)
	got := parseAlternates(rooted, '\n', resolved)
	if len(got) != 1 {
		t.Fatalf("parseAlternates(%q) = %q", rooted, got)
	}
	want := filepath.Join(resolved, rooted)
	if runtime.GOOS == "windows" {
		want = filepath.Join(resolved, "rooted")
	}
	gotInfo, err := os.Stat(got[0])
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("parseAlternates(%q) = %q, want %q", rooted, got[0], want)
	}
}

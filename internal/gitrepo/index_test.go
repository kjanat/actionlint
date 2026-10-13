package gitrepo

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	gitFlagIntentToAdd  = 1 << 29
	gitFlagSkipWorktree = 1 << 30
)

func (r *testRepo) gitInput(input string, args ...string) string {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	command.Stdin = strings.NewReader(input)
	out, err := command.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (r *testRepo) gitFails(args ...string) (string, bool) {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", args...)
	command.Dir = r.root
	out, err := command.CombinedOutput()
	return string(out), err != nil
}

func (r *testRepo) gitIndex(args ...string) []IndexEntry {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), "git", append([]string{"ls-files", "--debug", "--format=%(objectmode) %(objectname) %(stage) %(path)"}, args...)...)
	command.Dir = r.root
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		r.t.Fatalf("git ls-files: %v: %s", err, stderr.Bytes())
	}
	var entries []IndexEntry
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "  "); ok {
			_, flags, ok := strings.Cut(rest, "flags: ")
			if !ok {
				continue
			}
			value, err := strconv.ParseUint(flags, 16, 32)
			if err != nil || len(entries) == 0 {
				r.t.Fatalf("bad debug line %q", line)
			}
			entries[len(entries)-1].IntentToAdd = value&gitFlagIntentToAdd != 0
			entries[len(entries)-1].SkipWorktree = value&gitFlagSkipWorktree != 0
			continue
		}
		fields := strings.SplitN(line, " ", 4)
		if len(fields) != 4 {
			r.t.Fatalf("bad ls-files line %q", line)
		}
		mode, err := strconv.ParseUint(fields[0], 8, 32)
		if err != nil {
			r.t.Fatalf("bad mode in %q", line)
		}
		stage, err := strconv.ParseUint(fields[2], 10, 8)
		if err != nil {
			r.t.Fatalf("bad stage in %q", line)
		}
		path := fields[3]
		if path == "./" {
			path = ""
		}
		entries = append(entries, IndexEntry{Path: path, Mode: uint32(mode), Stage: uint8(stage), ObjectID: fields[1]})
	}
	return entries
}

func (r *testRepo) compareIndex() []IndexEntry {
	r.t.Helper()
	repo := r.open()
	raw, err := repo.Index()
	if err != nil {
		r.t.Fatal(err)
	}
	if want := r.gitIndex("--sparse"); !slices.Equal(raw, want) {
		r.t.Fatalf("Index =\n%v\nwant git ls-files --sparse\n%v", raw, want)
	}
	expanded, err := repo.ExpandedIndex(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	if want := r.gitIndex(); !slices.Equal(expanded, want) {
		r.t.Fatalf("ExpandedIndex =\n%v\nwant git ls-files\n%v", expanded, want)
	}
	tracked, err := repo.TrackedFiles(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	var want []string
	for _, entry := range expanded {
		if entry.IsFile() {
			want = append(want, entry.Path)
		}
	}
	slices.Sort(want)
	if want = slices.Compact(want); !slices.Equal(tracked, want) {
		r.t.Fatalf("TrackedFiles = %q, want %q", tracked, want)
	}
	return expanded
}

func (r *testRepo) writeFiles(names ...string) {
	r.t.Helper()
	for _, name := range names {
		r.write(name, name+"\n")
	}
}

func TestIndexMatchesGit(t *testing.T) {
	files := []string{"a/b/c.txt", "a/b/d.txt", "a/e.txt", "z.txt", "ab.txt"}
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
		{"version 4 skip worktree", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--index-version", "4")
			fixture.git("update-index", "--skip-worktree", "a/e.txt")
		}},
		{"sha256", []string{"--object-format=sha256"}, func(*testRepo) {}},
		{"untracked cache", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--untracked-cache")
		}},
		{"intent to add", nil, func(fixture *testRepo) {
			fixture.writeFiles("intent.sh", "a/intent.txt")
			fixture.git("add", "-N", "intent.sh", "a/intent.txt")
		}},
		{"intent to add version 4", nil, func(fixture *testRepo) {
			fixture.git("update-index", "--index-version", "4")
			fixture.writeFiles("intent.sh")
			fixture.git("add", "-N", "intent.sh")
		}},
		{"conflicts", nil, func(fixture *testRepo) {
			blob := strings.TrimSpace(fixture.gitInput("conflict\n", "hash-object", "-w", "--stdin"))
			fixture.gitInput("100644 "+blob+" 1\tconflict.txt\n100644 "+blob+" 2\tconflict.txt\n100755 "+blob+" 3\tconflict.txt\n0 0000000000000000000000000000000000000000\tz.txt\n100644 "+blob+" 2\tz.txt\n", "update-index", "--index-info")
		}},
		{"kinds", nil, func(fixture *testRepo) {
			if err := os.Symlink("z.txt", filepath.Join(fixture.root, "link")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			fixture.git("add", "link")
			fixture.git("update-index", "--chmod=+x", "a/e.txt")
			fixture.git("update-index", "--add", "--cacheinfo", "160000,4b825dc642cb6eb9a060e54bf8d69288fbee4904,sub")
		}},
		{"skip hash", nil, func(fixture *testRepo) {
			fixture.git("config", "index.skipHash", "true")
			fixture.git("update-index", "--force-write-index")
		}},
		{"many files", nil, func(fixture *testRepo) {
			fixture.git("config", "feature.manyFiles", "true")
			fixture.writeFiles("m/one.txt", "m/two.txt")
			fixture.git("add", "m")
		}},
		{"index threads", nil, func(fixture *testRepo) {
			fixture.git("config", "index.threads", "4")
			fixture.git("config", "index.recordOffsetTable", "true")
			fixture.git("config", "index.recordEndOfIndexEntries", "true")
			fixture.git("update-index", "--index-version", "4")
			fixture.git("update-index", "--force-write-index")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTestRepo(t, tc.initArgs...)
			fixture.writeFiles(files...)
			fixture.git("add", "--all")
			tc.setup(fixture)
			fixture.compareIndex()
		})
	}
}

func TestIndexSkipHashTrailer(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "index.skipHash", "true")
	fixture.writeFiles("file")
	fixture.git("add", "file")
	data, err := os.ReadFile(filepath.Join(fixture.root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[len(data)-20:], make([]byte, 20)) {
		t.Fatalf("git wrote checksum %x with index.skipHash", data[len(data)-20:])
	}
	fixture.compareIndex()
}

func TestIndexSplit(t *testing.T) {
	for _, version := range []string{"2", "3", "4"} {
		t.Run("version "+version, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.git("config", "core.splitIndex", "true")
			fixture.git("config", "splitIndex.maxPercentChange", "100")
			fixture.writeFiles("a/kept.txt", "a/removed.txt", "a/changed.txt", "b/kept.txt", "b/conflict.txt")
			fixture.git("add", "--all")
			fixture.git("update-index", "--index-version", version)
			fixture.git("update-index", "--split-index")
			fixture.git("rm", "--quiet", "--cached", "a/removed.txt")
			fixture.write("a/changed.txt", "changed\n")
			fixture.writeFiles("c/added.txt", "0first.txt", "b/added.txt")
			fixture.git("add", "a/changed.txt", "c/added.txt", "0first.txt", "b/added.txt")
			fixture.git("update-index", "--chmod=+x", "b/kept.txt")
			if version != "2" {
				fixture.git("update-index", "--skip-worktree", "a/kept.txt")
			}
			blob := strings.TrimSpace(fixture.gitInput("conflict\n", "hash-object", "-w", "--stdin"))
			fixture.gitInput("0 0000000000000000000000000000000000000000\tb/conflict.txt\n100644 "+blob+" 1\tb/conflict.txt\n100644 "+blob+" 3\tb/conflict.txt\n", "update-index", "--index-info")
			fixture.writeFiles("intent.txt")
			fixture.git("add", "-N", "intent.txt")
			index := fixture.open().IndexPath()
			data, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parseIndex(data, 20)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.link == nil || countBits(t, parsed.link.deleted) == 0 || countBits(t, parsed.link.replaced) == 0 {
				t.Fatalf("the index has no link extension with deletions and replacements: %+v", parsed.link)
			}
			fixture.compareIndex()
		})
	}
}

func countBits(t *testing.T, bitmap ewahBitmap) int {
	t.Helper()
	count := 0
	if err := bitmap.each(func(uint64) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestIndexSplitWithSkipHash(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "index.skipHash", "true")
	fixture.git("config", "splitIndex.maxPercentChange", "100")
	fixture.writeFiles("a", "b")
	fixture.git("add", "a", "b")
	fixture.git("update-index", "--split-index")
	fixture.writeFiles("c")
	fixture.git("add", "c")
	if _, err := os.Stat(filepath.Join(fixture.root, ".git", "sharedindex."+strings.Repeat("0", 40))); err != nil {
		t.Fatalf("git did not name the shared index after the null hash: %v", err)
	}
	entries := fixture.compareIndex()
	if len(entries) == 0 || entries[len(entries)-1].Path != "c" {
		t.Fatalf("entries = %v, want c last", entries)
	}
	for _, entry := range entries[:len(entries)-1] {
		if entry.Path != "" {
			t.Fatalf("entries = %v, want only unnamed replacements before c", entries)
		}
	}
}

func TestTrackedFilesMissingIndex(t *testing.T) {
	fixture := newTestRepo(t)
	got, err := fixture.open().TrackedFiles(t.Context())
	if err != nil || got != nil {
		t.Fatalf("TrackedFiles = %q, %v, want nil, nil", got, err)
	}
	if got := fixture.git("ls-files", "--stage"); got != "" {
		t.Fatalf("git ls-files --stage = %q", got)
	}
}

func TestIndexMissingSharedIndex(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.writeFiles("file")
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
	if out, failed := fixture.gitFails("ls-files"); !failed {
		t.Fatalf("git ls-files accepted the missing shared index: %s", out)
	}
	if got, err := fixture.open().Index(); err == nil {
		t.Fatalf("Index = %v, want an error for the missing shared index", got)
	}
}

func fixedEntry(version, count uint32, flags uint16) []byte {
	out := []byte("DIRC")
	out = binary.BigEndian.AppendUint32(out, version)
	out = binary.BigEndian.AppendUint32(out, count)
	out = append(out, make([]byte, 60)...)
	return binary.BigEndian.AppendUint16(out, flags)
}

func checksummed(parts ...[]byte) []byte {
	out := slices.Concat(parts...)
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func TestIndexSharedIndexUnreadable(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, path string){
		"directory": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0750); err != nil {
				t.Fatal(err)
			}
		},
		"signature": func(t *testing.T, path string) {
			patchFile(t, path, func(d []byte) []byte { d[0] = 'X'; return d })
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			fixture.writeFiles("file")
			fixture.git("add", "file")
			fixture.git("update-index", "--split-index")
			matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("shared indexes %v: %v", matches, err)
			}
			damage(t, matches[0])
			if out, failed := fixture.gitFails("ls-files"); !failed {
				t.Fatalf("git ls-files accepted the damaged shared index: %s", out)
			}
			if got, err := fixture.open().Index(); err == nil {
				t.Fatalf("Index = %v, want an error", got)
			}
		})
	}
}

func TestIndexSplitAdditionsNeedPathRules(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.git("config", "splitIndex.maxPercentChange", "100")
	fixture.writeFiles("a")
	fixture.git("add", "a")
	fixture.git("update-index", "--split-index")
	fixture.writeFiles("b")
	fixture.git("add", "b")
	repo := fixture.open()
	fixture.git("config", "core.protectNTFS", "maybe")
	if out, failed := fixture.gitFails("ls-files"); !failed {
		t.Fatalf("git ls-files accepted core.protectNTFS=maybe: %s", out)
	}
	if got, err := repo.Index(); err == nil {
		t.Fatalf("Index = %v, want an error for core.protectNTFS=maybe", got)
	}
	if got, err := repo.TrackedFiles(t.Context()); err == nil {
		t.Fatalf("TrackedFiles = %v, want an error for core.protectNTFS=maybe", got)
	}
	fixture.git("config", "core.protectNTFS", "true")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "maybe")
	if out, failed := fixture.gitFails("ls-files"); !failed {
		t.Fatalf("git ls-files accepted GIT_CONFIG_NOSYSTEM=maybe: %s", out)
	}
	if got, err := repo.Index(); err == nil {
		t.Fatalf("Index = %v, want an error for GIT_CONFIG_NOSYSTEM=maybe", got)
	}
}

func TestIndexSharedIndexChecksumMismatch(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.writeFiles("file")
	fixture.git("add", "file")
	fixture.git("update-index", "--split-index")
	matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("shared indexes %v: %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(matches[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, failed := fixture.gitFails("ls-files"); !failed || !strings.Contains(out, "broken index") {
		t.Fatalf("git ls-files = %q, %v, want broken index", out, failed)
	}
	if got, err := fixture.open().Index(); err == nil || !strings.Contains(err.Error(), "broken index") {
		t.Fatalf("Index = %v, %v, want broken index", got, err)
	}
}

type rawIndexEntry struct {
	mode     uint32
	flags    uint16
	extended uint16
	name     []byte
	oid      []byte
}

func encodeIndex(version uint32, entries []rawIndexEntry, tail ...[]byte) []byte {
	out := []byte("DIRC")
	out = binary.BigEndian.AppendUint32(out, version)
	out = binary.BigEndian.AppendUint32(out, uint32(len(entries)))
	for _, entry := range entries {
		start := len(out)
		out = append(out, make([]byte, indexStatBeforeMode)...)
		out = binary.BigEndian.AppendUint32(out, entry.mode)
		out = append(out, make([]byte, indexStatAfterMode)...)
		out = append(out, entry.oid...)
		out = append(out, make([]byte, 20-len(entry.oid))...)
		out = binary.BigEndian.AppendUint16(out, entry.flags)
		if entry.flags&indexExtendedFlag != 0 {
			out = binary.BigEndian.AppendUint16(out, entry.extended)
		}
		out = append(append(out, entry.name...), 0)
		if version != 4 {
			length := len(out) - start
			out = append(out, make([]byte, (length+7)/8*8-length)...)
		}
	}
	for _, part := range tail {
		out = append(out, part...)
	}
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func extension(signature string, payload []byte) []byte {
	return append(binary.BigEndian.AppendUint32([]byte(signature), uint32(len(payload))), payload...)
}

func plain(name string) rawIndexEntry {
	return rawIndexEntry{mode: 0o100644, flags: uint16(len(name)), name: []byte(name)}
}

func v4(strip []byte, suffix string, length int) rawIndexEntry {
	return rawIndexEntry{mode: 0o100644, flags: uint16(length), name: append(slices.Clone(strip), suffix...)}
}

func TestIndexCorruptionMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		data     []byte
		gitFails bool
	}{
		"short":                 {[]byte("DIRC"), true},
		"signature":             {append([]byte("NOPE\x00\x00\x00\x02\x00\x00\x00\x00"), make([]byte, 20)...), true},
		"version 1":             {encodeIndex(1, nil), true},
		"version 5":             {encodeIndex(5, nil), true},
		"lowercase extension":   {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("abcd", nil)), true},
		"link too short":        {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("link", nil)), true},
		"link garbage":          {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("link", append(make([]byte, 20), bitmap(0)[:6]...))), true},
		"link replace garbage":  {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("link", slices.Concat(make([]byte, 20), bitmap(0), bitmap(0)[:9]))), true},
		"link trailing garbage": {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("link", slices.Concat(make([]byte, 20), bitmap(0), bitmap(0), []byte{1}))), true},
		"unknown extended flag": {encodeIndex(3, []rawIndexEntry{{mode: 0o100644, flags: indexExtendedFlag | 1, extended: 0x8000, name: []byte("a")}}), true},
		"strip beyond previous": {encodeIndex(4, []rawIndexEntry{v4([]byte{0}, "ab", 2), v4([]byte{3}, "c", 1)}), true},
		"bad checksum":          {flipChecksum(encodeIndex(2, []rawIndexEntry{plain("a"), plain("b")})), false},
		"zero checksum":         {zeroChecksum(encodeIndex(2, []rawIndexEntry{plain("a")})), false},
		"trailing junk":         {encodeIndex(2, []rawIndexEntry{plain("a")}, []byte("abc")), false},
		"uppercase extension":   {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("ZZZZ", []byte("payload"))), false},
		"tree extension":        {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("TREE", []byte("garbage"))), false},
		"null link":             {encodeIndex(2, []rawIndexEntry{plain("a")}, extension("link", make([]byte, 20))), false},
		"masked name length":    {encodeIndex(2, []rawIndexEntry{{mode: 0o100644, flags: indexNameLengthMask, name: []byte("short")}}), false},
		"version 4 first strip": {encodeIndex(4, []rawIndexEntry{v4([]byte{5}, "ab", 2), v4([]byte{1}, "c", 2)}), false},
		"version 4 masked":      {encodeIndex(4, []rawIndexEntry{v4([]byte{0}, "ab", 2), {mode: 0o100644, flags: indexNameLengthMask, name: []byte{1, 'c'}}}), false},
		"unusual mode":          {encodeIndex(2, []rawIndexEntry{{mode: 0o100664, flags: 1, name: []byte("a")}, {mode: 0o777777, flags: 1, name: []byte("b")}}), false},
		"version 2 extended":    {encodeIndex(2, []rawIndexEntry{{mode: 0o100644, flags: indexExtendedFlag | 1, extended: indexSkipWorktreeFlag, name: []byte("a")}}), false},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTestRepo(t)
			if err := os.WriteFile(filepath.Join(fixture.root, ".git", "index"), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			out, failed := fixture.gitFails("ls-files", "--stage")
			if failed != tc.gitFails {
				t.Fatalf("git ls-files failed = %v, want %v: %s", failed, tc.gitFails, out)
			}
			if tc.gitFails {
				repo := fixture.open()
				if got, err := repo.Index(); err == nil {
					t.Fatalf("Index = %v, want an error", got)
				}
				if got, err := repo.ExpandedIndex(t.Context()); err == nil {
					t.Fatalf("ExpandedIndex = %v, want an error", got)
				}
				return
			}
			fixture.compareIndex()
		})
	}
}

func TestIndexDirectoryEntryWithoutTree(t *testing.T) {
	fixture := newTestRepo(t)
	data := encodeIndex(3, []rawIndexEntry{plain("a"), {mode: 0o040000, flags: indexExtendedFlag | 4, extended: indexSkipWorktreeFlag, name: []byte("dir/")}, plain("z")})
	if err := os.WriteFile(filepath.Join(fixture.root, ".git", "index"), data, 0600); err != nil {
		t.Fatal(err)
	}
	repo := fixture.open()
	raw, err := repo.Index()
	if err != nil || len(raw) != 3 || !raw[1].IsSparseDirectory() || !raw[1].SkipWorktree {
		t.Fatalf("Index = %v, %v, want a, the dir/ entry and z", raw, err)
	}
	expanded, err := repo.ExpandedIndex(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := fixture.gitIndex(); len(want) != 2 || !slices.Equal(expanded, want) {
		t.Fatalf("ExpandedIndex = %v, want git ls-files %v", expanded, want)
	}
}

func flipChecksum(data []byte) []byte {
	data[len(data)-1] ^= 1
	return data
}

func zeroChecksum(data []byte) []byte {
	copy(data[len(data)-20:], make([]byte, 20))
	return data
}

func TestIndexRejectsCorruption(t *testing.T) {
	overflow := append(bytes.Repeat([]byte{0xff}, 10), 0x7f)
	for name, data := range map[string][]byte{
		"truncated entry":          append([]byte("DIRC\x00\x00\x00\x02\x00\x00\x00\x01"), make([]byte, 20)...),
		"overflowing prefix":       encodeIndex(4, []rawIndexEntry{v4([]byte{0}, "ab", 2), v4(overflow, "c", 1)}),
		"overflowing first prefix": encodeIndex(4, []rawIndexEntry{v4(overflow, "c", 1)}),
		"length mismatch":          encodeIndex(2, []rawIndexEntry{{mode: 0o100644, flags: 3, name: []byte("ab")}}),
		"version 4 length":         encodeIndex(4, []rawIndexEntry{v4([]byte{0}, "ab", 5)}),
		"unterminated name":        checksummed(fixedEntry(2, 1, 3), []byte("abc")),
		"truncated extended flags": checksummed(fixedEntry(3, 1, indexExtendedFlag|1)),
		"truncated prefix":         checksummed(fixedEntry(4, 1, 1)),
		"truncated prefix byte":    checksummed(fixedEntry(4, 1, 1), []byte{0x80}),
		"missing padding":          checksummed(fixedEntry(2, 1, 2), []byte("ab\x00")),
		"unterminated suffix":      checksummed(fixedEntry(4, 1, 1), []byte{0, 'a'}),
		"extension beyond file":    encodeIndex(2, nil, binary.BigEndian.AppendUint32([]byte("ZZZZ"), 100)),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := readIndex(data, indexSource{hashLength: 20}); err == nil {
				t.Fatalf("readIndex = %v, want an error", got)
			}
		})
	}
}

func splitIndexFixture(t *testing.T) (*testRepo, []byte) {
	t.Helper()
	fixture := newTestRepo(t)
	fixture.writeFiles("a", "b")
	fixture.git("add", "a", "b")
	fixture.git("update-index", "--split-index")
	matches, err := filepath.Glob(filepath.Join(fixture.root, ".git", "sharedindex.*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("shared indexes %v: %v", matches, err)
	}
	shared, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return fixture, shared[len(shared)-20:]
}

func TestIndexSplitCorruptionMatchesGit(t *testing.T) {
	literal := func(bits uint64) []byte { return bitmap(64, marker(0, 0, 1), bits) }
	empty := bitmap(0)
	for name, tc := range map[string]struct {
		entries  []rawIndexEntry
		deleted  []byte
		replaced []byte
		gitFails bool
	}{
		"replacement with a name":   {[]rawIndexEntry{plain("a")}, empty, literal(0b1), true},
		"addition without a name":   {[]rawIndexEntry{{mode: 0o100644}}, empty, empty, true},
		"too many replacements":     {[]rawIndexEntry{{mode: 0o100755}}, empty, literal(0b11), true},
		"replacement beyond base":   {[]rawIndexEntry{{mode: 0o100755}}, empty, literal(0b100), true},
		"deletion beyond base":      {nil, literal(0b100), empty, true},
		"replaced and deleted":      {[]rawIndexEntry{{mode: 0o100755}}, literal(0b1), literal(0b1), false},
		"addition sorts first":      {[]rawIndexEntry{plain("0")}, empty, empty, false},
		"addition replaces base":    {[]rawIndexEntry{{mode: 0o100755, flags: 1, name: []byte("b")}}, empty, empty, false},
		"conflict addition":         {[]rawIndexEntry{{mode: 0o100755, flags: 1<<indexStageShift | 1, name: []byte("a")}}, empty, empty, false},
		"stage zero clears stages":  {[]rawIndexEntry{{mode: 0o100755, flags: 2<<indexStageShift | 1, name: []byte("c")}, plain("c")}, empty, empty, false},
		"invalid addition is lost":  {[]rawIndexEntry{plain(".git/config"), plain("x//y"), plain(".GIT"), plain("z")}, empty, empty, false},
		"ntfs short name is lost":   {[]rawIndexEntry{plain("GIT~1/config"), plain("d/.git. /x"), plain("e")}, empty, empty, false},
		"deletion of every entry":   {nil, literal(0b11), empty, false},
		"replacement keeps flags":   {[]rawIndexEntry{{mode: 0o100755, flags: indexExtendedFlag | 2<<indexStageShift, extended: indexIntentToAddFlag}}, empty, literal(0b10), false},
		"run of ones":               {nil, bitmap(2, marker(1, 1, 0)), empty, true},
		"bits past the bit count":   {nil, bitmap(1, marker(0, 0, 1), 0b10), empty, false},
		"replacements in bit order": {[]rawIndexEntry{{mode: 0o100755}, {mode: 0o120000}}, empty, literal(0b11), false},
	} {
		t.Run(name, func(t *testing.T) {
			fixture, base := splitIndexFixture(t)
			link := slices.Concat(base, tc.deleted, tc.replaced)
			data := encodeIndex(2, tc.entries, extension("link", link))
			if err := os.WriteFile(filepath.Join(fixture.root, ".git", "index"), data, 0600); err != nil {
				t.Fatal(err)
			}
			out, failed := fixture.gitFails("ls-files", "--stage")
			if failed != tc.gitFails {
				t.Fatalf("git ls-files failed = %v, want %v: %s", failed, tc.gitFails, out)
			}
			if tc.gitFails {
				if got, err := fixture.open().Index(); err == nil {
					t.Fatalf("Index = %v, want an error", got)
				}
				return
			}
			fixture.compareIndex()
		})
	}
}

func TestPathRules(t *testing.T) {
	for _, tc := range []struct {
		path  string
		mode  uint32
		rules pathRules
		want  bool
	}{
		{"a/b", 0o100644, pathRules{}, true},
		{"", 0o100644, pathRules{}, false},
		{"/a", 0o100644, pathRules{}, false},
		{"a/", 0o100644, pathRules{}, false},
		{"a/", 0o040000, pathRules{}, true},
		{"a//b", 0o100644, pathRules{}, false},
		{"./a", 0o100644, pathRules{}, false},
		{"a/../b", 0o100644, pathRules{}, false},
		{"a/.Git/b", 0o100644, pathRules{}, false},
		{".gitignore", 0o100644, pathRules{}, true},
		{".gitmodules", 0o120000, pathRules{}, false},
		{".gitmodules", 0o100644, pathRules{}, true},
		{"..a", 0o100644, pathRules{}, true},
		{"git~1/x", 0o100644, pathRules{}, true},
		{"git~1/x", 0o100644, pathRules{ntfs: true}, false},
		{".git . /x", 0o100644, pathRules{ntfs: true}, false},
		{".git:stream", 0o100644, pathRules{ntfs: true}, false},
		{"a\\.git\\x", 0o100644, pathRules{ntfs: true}, false},
		{"a\\.git\\x", 0o100644, pathRules{}, true},
		{"gitmod~4", 0o120000, pathRules{ntfs: true}, false},
		{"gi7eba~9", 0o120000, pathRules{ntfs: true}, false},
		{"gi7eba~9", 0o100644, pathRules{ntfs: true}, true},
		{".gitmodules ...", 0o120000, pathRules{ntfs: true}, false},
		{".g\u200cit/x", 0o100644, pathRules{hfs: true}, false},
		{".g\u200cit/x", 0o100644, pathRules{}, true},
		{".gitmodules\ufeff", 0o120000, pathRules{hfs: true}, false},
		{".gitx", 0o100644, pathRules{hfs: true, ntfs: true}, true},
		{".gitmodulesx", 0o120000, pathRules{ntfs: true}, true},
		{"gi7eb", 0o120000, pathRules{ntfs: true}, true},
		{"gi7~1abc", 0o120000, pathRules{ntfs: true}, true},
		{"gi7e~0", 0o120000, pathRules{ntfs: true}, true},
		{"gi7eba~12", 0o120000, pathRules{ntfs: true}, true},
		{"gi7eb~12", 0o120000, pathRules{ntfs: true}, false},
		{"gi7ebaz~1", 0o120000, pathRules{ntfs: true}, true},
		{"gi\xe9", 0o120000, pathRules{ntfs: true}, true},
		{"gx", 0o120000, pathRules{ntfs: true}, true},
		{"a/b", 0o100644, pathRules{hfs: true}, true},
		{".gxt/a", 0o100644, pathRules{hfs: true}, true},
		{".g\u00e9/a", 0o100644, pathRules{hfs: true}, true},
		{".git\xff", 0o100644, pathRules{hfs: true}, false},
	} {
		if got := tc.rules.valid([]byte(tc.path), tc.mode); got != tc.want {
			t.Errorf("%+v.valid(%q, %o) = %v, want %v", tc.rules, tc.path, tc.mode, got, tc.want)
		}
	}
}

func TestPathRulesMatchGit(t *testing.T) {
	fixture := newTestRepo(t)
	blob := strings.TrimSpace(fixture.gitInput("x\n", "hash-object", "-w", "--stdin"))
	for _, tc := range []struct {
		path   string
		config []string
	}{
		{"git~1/x", nil},
		{"git~1/x", []string{"-c", "core.protectNTFS=false"}},
		{".g\u200cit/x", []string{"-c", "core.protectHFS=true"}},
		{".g\u200cit/x", []string{"-c", "core.protectHFS=false"}},
		{"a/.GIT/x", nil},
		{".gitignore", nil},
	} {
		args := append(slices.Clone(tc.config), "update-index", "--add", "--cacheinfo", "100644,"+blob+","+tc.path)
		_, failed := fixture.gitFails(args...)
		rules := pathRules{ntfs: !slices.Contains(tc.config, "core.protectNTFS=false"), hfs: slices.Contains(tc.config, "core.protectHFS=true")}
		if got := rules.valid([]byte(tc.path), 0o100644); got == failed {
			t.Errorf("valid(%q) with %v = %v, git update-index failed = %v", tc.path, tc.config, got, failed)
		}
	}
}

func TestExpandedIndexCancelled(t *testing.T) {
	fixture := newTestRepo(t)
	fixture.write("file", "")
	fixture.git("add", "file")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := fixture.open().ExpandedIndex(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExpandedIndex = %v, %v, want context.Canceled", got, err)
	}
	if got, err := fixture.open().TrackedFiles(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("TrackedFiles = %v, %v, want context.Canceled", got, err)
	}
}

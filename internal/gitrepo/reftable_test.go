package gitrepo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type reftableRecord struct {
	prefix int
	suffix string
	kind   uint64
	value  []byte
}

func reftableVarintBytes(value uint64) []byte {
	out := []byte{byte(value & 0x7f)}
	for value >>= 7; value > 0; value >>= 7 {
		value--
		out = append([]byte{byte(value&0x7f) | 0x80}, out...)
	}
	return out
}

func reftableRecordBytes(records []reftableRecord) []byte {
	var data []byte
	for _, record := range records {
		data = append(data, reftableVarintBytes(uint64(record.prefix))...)
		data = append(data, reftableVarintBytes(uint64(len(record.suffix))<<3|record.kind)...)
		data = append(data, record.suffix...)
		data = append(data, reftableVarintBytes(0)...)
		if record.kind == reftableSymref {
			data = append(data, reftableVarintBytes(uint64(len(record.value)))...)
		}
		data = append(data, record.value...)
	}
	return data
}

func reftableHeader(version byte, hashID string, blockSize int) []byte {
	data := append([]byte("REFT"), version, byte(blockSize>>16), byte(blockSize>>8), byte(blockSize))
	data = append(data, make([]byte, 16)...)
	if version == 2 {
		data = append(data, hashID...)
	}
	return data
}

func reftableBlock(kind byte, payload []byte, restarts int) []byte {
	data := append([]byte{kind, 0, 0, 0}, payload...)
	for range restarts {
		data = append(data, 0, 0, 4)
	}
	data = append(data, byte(restarts>>8), byte(restarts))
	data[1], data[2], data[3] = byte(len(data)>>16), byte(len(data)>>8), byte(len(data))
	return data
}

type reftablePositions struct{ refIndex, obj, log uint64 }

func reftableFooter(data []byte, positions reftablePositions) []byte {
	header := reftableHeaderV1
	if data[4] == 2 {
		header = reftableHeaderV2
	}
	footer := slices.Clone(data[:header])
	footer = binary.BigEndian.AppendUint64(footer, positions.refIndex)
	footer = binary.BigEndian.AppendUint64(footer, positions.obj<<5)
	footer = binary.BigEndian.AppendUint64(footer, 0)
	footer = binary.BigEndian.AppendUint64(footer, positions.log)
	footer = binary.BigEndian.AppendUint64(footer, 0)
	return append(data, binary.BigEndian.AppendUint32(footer, crc32.ChecksumIEEE(footer))...)
}

func reftableBody(version byte, hashID string, blockSize int, blocks ...[]reftableRecord) []byte {
	data := reftableHeader(version, hashID, blockSize)
	for i, records := range blocks {
		block := reftableBlock('r', reftableRecordBytes(records), 1)
		if i == 0 {
			size := len(data) + len(block)
			block[1], block[2], block[3] = byte(size>>16), byte(size>>8), byte(size)
		}
		data = append(data, block...)
		if blockSize > 0 {
			data = append(data, make([]byte, blockSize*(i+1)-len(data))...)
		}
	}
	return data
}

func reftableBlocks(version byte, hashID string, blockSize int, blocks ...[]reftableRecord) []byte {
	return reftableFooter(reftableBody(version, hashID, blockSize, blocks...), reftablePositions{})
}

func reftableFile(version byte, hashID string, records ...reftableRecord) []byte {
	return reftableBlocks(version, hashID, 0, records)
}

func reftableRaw(payload []byte, restarts int) []byte {
	data := reftableHeader(1, "", 0)
	block := reftableBlock('r', payload, restarts)
	size := len(data) + len(block)
	block[1], block[2], block[3] = byte(size>>16), byte(size>>8), byte(size)
	return reftableFooter(append(data, block...), reftablePositions{})
}

func TestReftableVarintRoundTrip(t *testing.T) {
	for _, value := range []uint64{0, 1, 127, 128, 300, 16511, 16512, 1 << 40} {
		got, n, ok := reftableVarint(append(reftableVarintBytes(value), 0xff))
		if !ok || got != value || n != len(reftableVarintBytes(value)) {
			t.Errorf("reftableVarint(%d) = %d, %d, %v", value, got, n, ok)
		}
	}
	for name, data := range map[string][]byte{"empty": nil, "unterminated": {0x80}, "overlong": {0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00}} {
		if _, _, ok := reftableVarint(data); ok {
			t.Errorf("reftableVarint(%s) succeeded", name)
		}
	}
}

func TestReftableRef(t *testing.T) {
	sha1 := make([]byte, 20)
	sha256 := make([]byte, 32)
	head := func(target string) reftableRecord {
		return reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte(target)}
	}
	filler := func(n int) []reftableRecord {
		var records []reftableRecord
		for i := range n {
			records = append(records, reftableRecord{suffix: "A_" + strings.Repeat("x", i%7) + string(rune('a'+i%26)), kind: 1, value: sha1})
		}
		return records
	}
	unpaddedSingle := reftableBody(1, "", 0, filler(30))
	unpaddedSingle[5], unpaddedSingle[6], unpaddedSingle[7] = 0, 0x10, 0
	restartOverflow := reftableRaw(nil, 1)
	restartOverflow[32] = 9
	badCRC := reftableFile(1, "", head("refs/heads/main"))
	badCRC[len(badCRC)-10] ^= 1
	for name, tc := range map[string]struct {
		data    []byte
		name    string
		target  string
		symref  bool
		found   bool
		present bool
		err     string
	}{
		"symref":          {data: reftableFile(1, "", head("refs/heads/main")), target: "refs/heads/main", symref: true, found: true, present: true},
		"detached":        {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 1, value: sha1}), found: true, present: true},
		"deleted":         {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 0}), present: true},
		"after peeled":    {data: reftableFile(1, "", reftableRecord{suffix: "CHERRY_PICK_HEAD", kind: 2, value: make([]byte, 40)}, head("refs/heads/x")), target: "refs/heads/x", symref: true, found: true, present: true},
		"prefix":          {data: reftableFile(1, "", reftableRecord{suffix: "HE", kind: 1, value: sha1}, reftableRecord{prefix: 2, suffix: "AD", kind: reftableSymref, value: []byte("refs/heads/p")}), target: "refs/heads/p", symref: true, found: true, present: true},
		"only branches":   {data: reftableFile(1, "", reftableRecord{suffix: "refs/heads/main", kind: 1, value: sha1})},
		"other name":      {data: reftableFile(1, "", head("refs/heads/main"), reftableRecord{suffix: "refs/meta/x", kind: reftableSymref, value: []byte("refs/heads/y")}), name: "refs/meta/x", target: "refs/heads/y", symref: true, found: true, present: true},
		"empty block":     {data: reftableFile(1, "")},
		"empty symref":    {data: reftableFile(1, "", head("")), symref: true, found: true, present: true},
		"sha256 symref":   {data: reftableFile(2, "s256", reftableRecord{suffix: "A", kind: 1, value: sha256}, head("refs/heads/s")), target: "refs/heads/s", symref: true, found: true, present: true},
		"sha1 v2":         {data: reftableFile(2, "sha1", reftableRecord{suffix: "A", kind: 2, value: make([]byte, 40)}, head("refs/heads/v")), target: "refs/heads/v", symref: true, found: true, present: true},
		"second block":    {data: reftableBlocks(1, "", 4096, filler(120), []reftableRecord{head("refs/heads/second")}), target: "refs/heads/second", symref: true, found: true, present: true},
		"third block":     {data: reftableBlocks(1, "", 1024, filler(30), filler(30), []reftableRecord{head("refs/heads/third")}), target: "refs/heads/third", symref: true, found: true, present: true},
		"unaligned":       {data: reftableBlocks(1, "", 0, filler(120), []reftableRecord{head("refs/heads/loose")}), target: "refs/heads/loose", symref: true, found: true, present: true},
		"passed in later": {data: reftableBlocks(1, "", 1024, filler(30), []reftableRecord{{suffix: "refs/heads/z", kind: 1, value: sha1}})},
		"ends at index":   {data: reftableFooter(slices.Concat(reftableBody(1, "", 1024, filler(30)), reftableBlock('i', nil, 1)), reftablePositions{refIndex: 1024})},
		"ends at objects": {data: reftableFooter(slices.Concat(reftableBody(1, "", 1024, filler(30)), reftableBlock('o', nil, 1)), reftablePositions{obj: 1024})},
		"ends at log":     {data: reftableFooter(slices.Concat(reftableBody(1, "", 1024, filler(30)), []byte{'g', 0, 0, 10, 'r', 0}), reftablePositions{log: 1024})},
		"unpadded single": {data: reftableFooter(unpaddedSingle, reftablePositions{})},
		"empty table":     {data: reftableFooter(reftableHeader(1, "", 0), reftablePositions{})},
		"not a reftable":  {data: []byte("not a reftable at all, long enough"), err: "not a reftable"},
		"short":           {data: []byte("REFT"), err: "not a reftable"},
		"version":         {data: reftableFile(3, "s256"), err: "unsupported reftable version 3"},
		"truncated":       {data: reftableFile(1, "")[:26], err: "truncated reftable"},
		"no footer":       {data: reftableBody(1, "", 1024, filler(30), filler(30)), err: "bad reftable footer"},
		"bad footer crc":  {data: badCRC, err: "bad reftable footer"},
		"log only":        {data: reftableFooter(slices.Concat(reftableHeader(1, "", 0), []byte{'g', 0, 0, 10, 0, 0}), reftablePositions{log: 24})},
		"block length":    {data: reftableFooter(slices.Concat(reftableHeader(1, "", 0), []byte{'r', 0xff, 0xff, 0xff, 0, 0}), reftablePositions{}), err: "ref block length out of range"},
		"over block size": {data: reftableFooter(slices.Concat(reftableHeader(1, "", 32), reftableBlock('r', reftableRecordBytes(filler(3)), 1)), reftablePositions{}), err: "ref block length out of range"},
		"no restarts":     {data: reftableRaw(nil, 0), err: "ref block restart table out of range"},
		"many restarts":   {data: restartOverflow, err: "ref block restart table out of range"},
		"bad varint":      {data: reftableRaw([]byte{0, 0x80}, 1), err: "bad ref record"},
		"bad index delta": {data: reftableRaw([]byte{0, 4<<3 | 1, 'H', 'E', 'A', 'D', 0x80}, 1), err: "bad ref record"},
		"bad prefix":      {data: reftableFile(1, "", reftableRecord{prefix: 3, suffix: "AD", kind: 0}), err: "bad ref record"},
		"long suffix":     {data: reftableRaw(slices.Concat([]byte{0}, reftableVarintBytes(200<<3|1), []byte{'H'}), 1), err: "bad ref record"},
		"long symref":     {data: reftableRaw([]byte{0, 4<<3 | reftableSymref, 'H', 'E', 'A', 'D', 0, 100}, 1), err: "bad symref record"},
		"huge symref":     {data: reftableRaw(slices.Concat([]byte{0, 4<<3 | reftableSymref, 'H', 'E', 'A', 'D', 0}, reftableVarintBytes(1<<63)), 1), err: "bad symref record"},
		"unknown type":    {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 5}), err: "unknown ref record type 5"},
		"short oid":       {data: reftableFile(2, "s256", reftableRecord{suffix: "HEAD", kind: 1, value: sha1}), err: "bad ref record"},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.name == "" {
				tc.name = "HEAD"
			}
			value, present, err := reftableRef(tc.data, tc.name)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("reftableRef = %+v, %v, %v; want error %q", value, present, err, tc.err)
				}
				return
			}
			want := refValue{target: tc.target, symref: tc.symref, found: tc.found}
			if err != nil || present != tc.present || value != want {
				t.Fatalf("reftableRef = %+v, %v, %v; want %+v, %v", value, present, err, want, tc.present)
			}
		})
	}
}

func TestReadReftableRef(t *testing.T) {
	gitDir := t.TempDir()
	repo := &Repository{GitDir: gitDir, CommonDir: gitDir, hashLength: 20, reftable: true}
	dir := filepath.Join(gitDir, "reftable")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	var reads []string
	record := func(path string) { reads = append(reads, path) }
	if value, err := repo.readReftableRef("HEAD", record); err != nil || value.found || reads != nil {
		t.Fatalf("missing tables.list = %+v, %v, read %q; want an empty stack", value, err, reads)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("tables.list", []byte("older.ref\nnewer.ref\n"))
	if _, err := repo.readReftableRef("HEAD", record); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing table: %v", err)
	}
	write("older.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/old")}))
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "refs/heads/old", kind: 1, value: make([]byte, 20)}))
	reads = nil
	branch, err := repo.branch(record)
	if err != nil || branch != "old" {
		t.Fatalf("branch = %q, %v", branch, err)
	}
	snapshot := []string{filepath.Join(dir, "tables.list"), filepath.Join(dir, "older.ref"), filepath.Join(dir, "newer.ref")}
	if want := slices.Concat(snapshot, snapshot); !slices.Equal(reads, want) {
		t.Fatalf("reads = %q, want %q", reads, want)
	}
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/remotes/origin/x")}))
	if branch, err := repo.branch(record); err != nil || branch != "" {
		t.Fatalf("branch with a remote symref = %q, %v", branch, err)
	}
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 0}))
	if branch, err := repo.branch(record); err != nil || branch != "" {
		t.Fatalf("branch with HEAD deleted in the newest table = %q, %v", branch, err)
	}
	write("newer.ref", []byte("garbage"))
	if _, err := repo.branch(record); err == nil || !strings.Contains(err.Error(), "newer.ref") {
		t.Fatalf("garbage table: %v", err)
	}
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/new")}))
	write("older.ref", []byte("garbage"))
	if _, err := repo.branch(record); err == nil || !strings.Contains(err.Error(), "older.ref") {
		t.Fatalf("a corrupt older table is not reported: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "tables.list")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tables.list"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.branch(record); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an unreadable tables.list: %v", err)
	}
}

type fakeReftableFS struct {
	files map[string][]byte
	lists [][]byte
	reads int
	now   time.Time
	slept []time.Duration
}

func (f *fakeReftableFS) loader() reftableLoader {
	return reftableLoader{
		readFile: func(path string) ([]byte, error) {
			if filepath.Base(path) == "tables.list" {
				list := f.lists[min(f.reads, len(f.lists)-1)]
				f.reads++
				return list, nil
			}
			if data, ok := f.files[filepath.Base(path)]; ok {
				return data, nil
			}
			return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
		},
		now:   func() time.Time { return f.now },
		sleep: func(d time.Duration) { f.slept = append(f.slept, d); f.now = f.now.Add(d) },
	}
}

func TestReftableLoaderRetries(t *testing.T) {
	table := reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/main")})
	t.Run("unchanged list", func(t *testing.T) {
		fake := &fakeReftableFS{lists: [][]byte{[]byte("gone.ref\n")}}
		if _, err := fake.loader().load("dir", func(string) {}); !errors.Is(err, fs.ErrNotExist) || len(fake.slept) != 0 {
			t.Fatalf("load = %v after %d sleeps, want a missing table without retrying", err, len(fake.slept))
		}
	})
	t.Run("compacted", func(t *testing.T) {
		fake := &fakeReftableFS{lists: [][]byte{[]byte("gone.ref\n"), []byte("new.ref\n")}, files: map[string][]byte{"new.ref": table}}
		var reads []string
		tables, err := fake.loader().load("dir", func(path string) { reads = append(reads, path) })
		if err != nil || len(tables) != 1 || tables[0].path != filepath.Join("dir", "new.ref") || len(fake.slept) != 1 {
			t.Fatalf("load = %v, %v after %d sleeps", tables, err, len(fake.slept))
		}
		if want := []string{filepath.Join("dir", "tables.list"), filepath.Join("dir", "new.ref")}; !slices.Equal(reads, want) {
			t.Fatalf("reads = %q, want %q", reads, want)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		var lists [][]byte
		for i := range 10000 {
			lists = append(lists, fmt.Appendf(nil, "gone%d.ref\n", i))
		}
		fake := &fakeReftableFS{lists: lists, now: time.Unix(0, 0)}
		_, err := fake.loader().load("dir", func(string) {})
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("load = %v, want the missing table after the deadline", err)
		}
		if len(fake.slept) < 3 || fake.now.Sub(time.Unix(0, 0)) < 3*time.Second {
			t.Fatalf("gave up after %d sleeps and %v", len(fake.slept), fake.now.Sub(time.Unix(0, 0)))
		}
		for i := 1; i < len(fake.slept); i++ {
			if fake.slept[i] <= fake.slept[i-1] {
				t.Fatalf("delays do not grow: %v", fake.slept)
			}
		}
	})
}

func TestReftableStack(t *testing.T) {
	main := &Repository{GitDir: "common", CommonDir: "common"}
	linked := &Repository{GitDir: filepath.Join("common", "worktrees", "wt"), CommonDir: "common"}
	for _, tc := range []struct {
		repo      *Repository
		name, dir string
		key       string
	}{
		{main, "HEAD", filepath.Join("common", "reftable"), "HEAD"},
		{main, "refs/heads/x", filepath.Join("common", "reftable"), "refs/heads/x"},
		{linked, "HEAD", filepath.Join("common", "worktrees", "wt", "reftable"), "HEAD"},
		{linked, "refs/bisect/bad", filepath.Join("common", "worktrees", "wt", "reftable"), "refs/bisect/bad"},
		{linked, "refs/heads/x", filepath.Join("common", "reftable"), "refs/heads/x"},
		{linked, "main-worktree/HEAD", filepath.Join("common", "reftable"), "HEAD"},
		{linked, "worktrees/other/HEAD", filepath.Join("common", "worktrees", "other", "reftable"), "HEAD"},
		{linked, "worktrees/other/refs/heads/x", filepath.Join("common", "reftable"), "worktrees/other/refs/heads/x"},
	} {
		if dir, key := tc.repo.reftableStack(tc.name); dir != tc.dir || key != tc.key {
			t.Errorf("reftableStack(%q) in %s = %s, %s; want %s, %s", tc.name, tc.repo.GitDir, dir, key, tc.dir, tc.key)
		}
	}
}

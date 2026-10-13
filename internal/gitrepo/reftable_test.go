package gitrepo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

func reftableFile(version byte, hashID string, records ...reftableRecord) []byte {
	data := append([]byte("REFT"), version, 0, 0x10, 0)
	data = append(data, make([]byte, 16)...)
	if version == 2 {
		data = append(data, hashID...)
	}
	start := len(data)
	data = append(data, 'r', 0, 0, 0)
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
	data = append(data, 0, 0, byte(start), 0, 1)
	data[start+1], data[start+2], data[start+3] = byte(len(data)>>16), byte(len(data)>>8), byte(len(data))
	return data
}

func reftableRaw(payload []byte, restarts int) []byte {
	data := reftableFile(1, "")[:reftableHeaderV1]
	data = append(data, 'r', 0, 0, 0)
	data = append(data, payload...)
	for range restarts {
		data = append(data, 0, 0, reftableHeaderV1+4)
	}
	data = append(data, byte(restarts>>8), byte(restarts))
	data[reftableHeaderV1+1], data[reftableHeaderV1+2], data[reftableHeaderV1+3] = byte(len(data)>>16), byte(len(data)>>8), byte(len(data))
	return data
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

func TestReftableHead(t *testing.T) {
	sha1 := make([]byte, 20)
	sha256 := make([]byte, 32)
	for name, tc := range map[string]struct {
		data   []byte
		target string
		found  bool
		err    string
	}{
		"symref":          {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/main")}), target: "refs/heads/main", found: true},
		"detached":        {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 1, value: sha1}), found: true},
		"deleted":         {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 0}), found: true},
		"after peeled":    {data: reftableFile(1, "", reftableRecord{suffix: "CHERRY_PICK_HEAD", kind: 2, value: make([]byte, 40)}, reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/x")}), target: "refs/heads/x", found: true},
		"prefix":          {data: reftableFile(1, "", reftableRecord{suffix: "HE", kind: 1, value: sha1}, reftableRecord{prefix: 2, suffix: "AD", kind: reftableSymref, value: []byte("refs/heads/p")}), target: "refs/heads/p", found: true},
		"only branches":   {data: reftableFile(1, "", reftableRecord{suffix: "refs/heads/main", kind: 1, value: sha1})},
		"empty block":     {data: reftableFile(1, "")},
		"sha256 symref":   {data: reftableFile(2, "s256", reftableRecord{suffix: "A", kind: 1, value: sha256}, reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/s")}), target: "refs/heads/s", found: true},
		"sha1 v2":         {data: reftableFile(2, "sha1", reftableRecord{suffix: "A", kind: 2, value: make([]byte, 40)}, reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/v")}), target: "refs/heads/v", found: true},
		"not a reftable":  {data: []byte("not a reftable at all, long enough"), err: "not a reftable"},
		"short":           {data: []byte("REFT"), err: "not a reftable"},
		"version":         {data: reftableFile(3, "s256"), err: "unsupported reftable version 3"},
		"truncated":       {data: reftableFile(1, "")[:26], err: "truncated reftable"},
		"log only":        {data: slices.Concat(reftableFile(1, "")[:24], []byte{'g', 0, 0, 10, 0, 0})},
		"block length":    {data: slices.Concat(reftableFile(1, "")[:24], []byte{'r', 0xff, 0xff, 0xff, 0, 0}), err: "ref block length out of range"},
		"no restarts":     {data: reftableRaw(nil, 0), err: "ref block restart table out of range"},
		"bad varint":      {data: reftableRaw([]byte{0, 0x80}, 1), err: "bad ref record"},
		"bad index delta": {data: reftableRaw([]byte{0, 4<<3 | 1, 'H', 'E', 'A', 'D', 0x80}, 1), err: "bad ref record"},
		"bad prefix":      {data: reftableFile(1, "", reftableRecord{prefix: 3, suffix: "AD", kind: 0}), err: "bad ref record"},
		"long symref":     {data: reftableRaw([]byte{0, 4<<3 | reftableSymref, 'H', 'E', 'A', 'D', 0, 100}, 1), err: "bad symref record"},
		"empty symref":    {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: nil}), found: true},
		"unknown type":    {data: reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: 5}), err: "unknown ref record type 5"},
		"short oid":       {data: reftableFile(2, "s256", reftableRecord{suffix: "HEAD", kind: 1, value: sha1}), err: "bad ref record"},
	} {
		t.Run(name, func(t *testing.T) {
			target, found, err := reftableHead(tc.data)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("reftableHead = %q, %v, %v; want error %q", target, found, err, tc.err)
				}
				return
			}
			if err != nil || found != tc.found || target != tc.target {
				t.Fatalf("reftableHead = %q, %v, %v; want %q, %v", target, found, err, tc.target, tc.found)
			}
		})
	}
}

func TestReftableBranch(t *testing.T) {
	gitDir := t.TempDir()
	dir := filepath.Join(gitDir, "reftable")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	var reads []string
	record := func(path string) { reads = append(reads, path) }
	if _, err := reftableBranch(gitDir, record); err == nil {
		t.Fatal("missing tables.list succeeded")
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("tables.list", []byte("older.ref\nnewer.ref\n"))
	if _, err := reftableBranch(gitDir, record); err == nil {
		t.Fatal("missing table succeeded")
	}
	write("older.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/heads/old")}))
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "refs/heads/old", kind: 1, value: make([]byte, 20)}))
	reads = nil
	branch, err := reftableBranch(gitDir, record)
	if err != nil || branch != "old" {
		t.Fatalf("reftableBranch = %q, %v", branch, err)
	}
	if want := []string{filepath.Join(dir, "tables.list"), filepath.Join(dir, "newer.ref"), filepath.Join(dir, "older.ref")}; !slices.Equal(reads, want) {
		t.Fatalf("reads = %q, want %q", reads, want)
	}
	write("newer.ref", reftableFile(1, "", reftableRecord{suffix: "HEAD", kind: reftableSymref, value: []byte("refs/remotes/origin/x")}))
	if branch, err := reftableBranch(gitDir, record); err != nil || branch != "" {
		t.Fatalf("reftableBranch with a remote symref = %q, %v", branch, err)
	}
	write("newer.ref", []byte("garbage"))
	if _, err := reftableBranch(gitDir, record); err == nil || !strings.Contains(err.Error(), "newer.ref") {
		t.Fatalf("garbage table: %v", err)
	}
}

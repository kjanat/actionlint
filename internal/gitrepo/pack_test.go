package gitrepo

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func (r *testRepo) matchGit() {
	r.t.Helper()
	out, failed := r.gitFails("ls-files", "--stage")
	got, err := r.open().ExpandedIndex(r.t.Context())
	if failed {
		if err == nil {
			r.t.Fatalf("git ls-files failed (%s) but ExpandedIndex = %v", out, got)
		}
		return
	}
	if err != nil {
		r.t.Fatalf("git ls-files succeeded but ExpandedIndex failed: %v", err)
	}
	r.compareIndex()
}

func patchFile(t *testing.T, path string, patch func([]byte) []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, patch(data), 0600); err != nil {
		t.Fatal(err)
	}
}

type packedRepo struct {
	*testRepo
	pack     string
	index    string
	tree     []byte
	position uint32
	offset   uint64
	header   int
}

func newPackedRepo(t *testing.T, args ...string) *packedRepo {
	t.Helper()
	fixture := sparseFixture(t)
	fixture.git(append(args, "repack", "-a", "-d", "-f", "-q", "--window=250", "--depth=50")...)
	fixture.git("prune-packed")
	packs, err := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.pack"))
	if err != nil || len(packs) != 1 {
		t.Fatalf("packs %v: %v", packs, err)
	}
	p := &packedRepo{testRepo: fixture, pack: packs[0], index: strings.TrimSuffix(packs[0], ".pack") + ".idx"}
	index, err := loadPackIndex(testDisk(t), p.index, 20)
	if err != nil {
		t.Fatal(err)
	}
	if p.tree, err = hex.DecodeString(fixture.treeOID("out/bulk")); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if p.position, ok = index.find(p.tree); !ok {
		t.Fatal("the out/bulk tree is not packed")
	}
	if p.offset, err = index.offset(p.position); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p.pack)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, p.header, err = packObjectHeader(data[p.offset:]); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *packedRepo) indexOffsetEntry(data []byte) []byte {
	count := int(binary.BigEndian.Uint32(data[8+4*255:]))
	start := 8 + 4*256 + count*24 + int(p.position)*4
	return data[start : start+4]
}

func TestExpandedIndexCorruptPackMatchesGit(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		mutate func(t *testing.T, p *packedRepo)
	}{
		"pack signature": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[0] = 'X'; return d })
		}},
		"pack version": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[7] = 4; return d })
		}},
		"pack object count": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[11]++; return d })
		}},
		"pack checksum": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[len(d)-1] ^= 1; return d })
		}},
		"pack too short": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { return d[:8] })
		}},
		"pack removed": {nil, func(t *testing.T, p *packedRepo) {
			if err := os.Remove(p.pack); err != nil {
				t.Fatal(err)
			}
		}},
		"index removed": {nil, func(t *testing.T, p *packedRepo) {
			if err := os.Remove(p.index); err != nil {
				t.Fatal(err)
			}
		}},
		"index version": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.index, func(d []byte) []byte { d[7] = 3; return d })
		}},
		"object type": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[p.offset] = d[p.offset]&0x8f | 5<<4; return d })
		}},
		"object size": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[p.offset] ^= 1; return d })
		}},
		"object data": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { d[p.offset+uint64(p.header)+6] ^= 0xff; return d })
		}},
		"offset delta base": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte {
				i := p.offset + uint64(p.header)
				for ; d[i]&0x80 != 0; i++ {
					d[i] = 0xff
				}
				d[i] = 0x7f
				return d
			})
		}},
		"index offset beyond pack": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.index, func(d []byte) []byte {
				binary.BigEndian.PutUint32(p.indexOffsetEntry(d), 0x7fff_fff0)
				return d
			})
		}},
		"index large offset": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.index, func(d []byte) []byte {
				binary.BigEndian.PutUint32(p.indexOffsetEntry(d), 0x8000_0005)
				return d
			})
		}},
		"pack unreadable": {nil, func(t *testing.T, p *packedRepo) {
			if err := os.Chmod(p.pack, 0); err != nil {
				t.Fatal(err)
			}
		}},
		"pack directory": {nil, func(t *testing.T, p *packedRepo) {
			if err := os.Remove(p.pack); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p.pack, 0750); err != nil {
				t.Fatal(err)
			}
		}},
		"pack header only": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte { return d[:20] })
		}},
		"object header": {nil, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte {
				copy(d[p.offset:], bytes.Repeat([]byte{0xff}, 16))
				return d
			})
		}},
		"chain base data": {nil, func(t *testing.T, p *packedRepo) {
			pack := newPackFile(testDisk(t), p.index)
			if err := pack.open(20); err != nil {
				t.Fatal(err)
			}
			offset := p.offset
			for {
				header, err := pack.window(offset)
				if err != nil {
					t.Fatal(err)
				}
				kind, _, used, err := packObjectHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				if kind != objectOffsetDelta {
					offset += uint64(used) + 6
					break
				}
				if offset, _, err = pack.deltaBase(kind, header[used:], offset); err != nil {
					t.Fatal(err)
				}
			}
			pack.close()
			patchFile(t, p.pack, func(d []byte) []byte { d[offset] ^= 0xff; return d })
		}},
		"wrong delta base": {nil, func(t *testing.T, p *packedRepo) {
			pack := newPackFile(testDisk(t), p.index)
			if err := pack.open(20); err != nil {
				t.Fatal(err)
			}
			header, err := pack.window(p.offset)
			if err != nil {
				t.Fatal(err)
			}
			base, used, err := pack.deltaBase(objectOffsetDelta, header[p.header:], p.offset)
			if err != nil {
				t.Fatal(err)
			}
			var target uint64
			for n := range pack.index.count {
				candidate, err := pack.index.offset(n)
				if err != nil {
					t.Fatal(err)
				}
				if candidate < p.offset && candidate != base && len(encodeOffset(p.offset-candidate)) == used {
					target = candidate
					break
				}
			}
			pack.close()
			if target == 0 {
				t.Fatal("no other object fits the base offset encoding")
			}
			patchFile(t, p.pack, func(d []byte) []byte {
				copy(d[p.offset+uint64(p.header):], encodeOffset(p.offset-target))
				return d
			})
		}},
		"reference delta base": {[]string{"-c", "repack.useDeltaBaseOffset=false"}, func(t *testing.T, p *packedRepo) {
			patchFile(t, p.pack, func(d []byte) []byte {
				copy(d[p.offset+uint64(p.header):], bytes.Repeat([]byte{0x42}, 20))
				return d
			})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			p := newPackedRepo(t, tc.args...)
			tc.mutate(t, p)
			p.matchGit()
		})
	}
}

func TestPackReferenceDeltaLoop(t *testing.T) {
	p := newPackedRepo(t, "-c", "repack.useDeltaBaseOffset=false")
	if kind := p.packedKind(hex.EncodeToString(p.tree)); kind != objectRefDelta {
		t.Fatalf("the out/bulk tree is packed as type %d", kind)
	}
	patchFile(t, p.pack, func(d []byte) []byte {
		copy(d[p.offset+uint64(p.header):], p.tree)
		return d
	})
	if got, err := p.open().ExpandedIndex(t.Context()); err == nil || !strings.Contains(err.Error(), "loops") {
		t.Fatalf("ExpandedIndex = %v, %v, want a delta loop error", got, err)
	}
}

func midxChunk(t *testing.T, data []byte, id string) (int, int, int) {
	t.Helper()
	count := int(data[6])
	for i := range count {
		entry := data[midxHeaderSize+i*midxChunkEntrySize:]
		if string(entry[:4]) == id {
			return midxHeaderSize + i*midxChunkEntrySize, int(binary.BigEndian.Uint64(entry[4:])), int(binary.BigEndian.Uint64(entry[midxChunkEntrySize+4:]))
		}
	}
	t.Fatalf("no %s chunk", id)
	return 0, 0, 0
}

func TestExpandedIndexCorruptMultiPackIndexMatchesGit(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, fixture *testRepo, midx string){
		"signature": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[0] = 'X'; return d })
		},
		"version": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[4] = 3; return d })
		},
		"hash version": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[5] = 2; return d })
		},
		"too small": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { return d[:20] })
		},
		"chunk alignment": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[midxHeaderSize+11]++; return d })
		},
		"chunk beyond file": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				entry, _, _ := midxChunk(t, d, midxChunkOffsets)
				binary.BigEndian.PutUint64(d[entry+midxChunkEntrySize+4:], uint64(len(d)))
				return d
			})
		},
		"terminator": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[midxHeaderSize+int(d[6])*midxChunkEntrySize] = 'X'; return d })
		},
		"early terminator": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { copy(d[midxHeaderSize:], []byte{0, 0, 0, 0}); return d })
		},
		"duplicate chunk": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				copy(d[midxHeaderSize+midxChunkEntrySize:], d[midxHeaderSize:midxHeaderSize+4])
				return d
			})
		},
		"missing offsets chunk": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				entry, _, _ := midxChunk(t, d, midxChunkOffsets)
				d[entry+3] = 'X'
				return d
			})
		},
		"missing pack names": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				entry, _, _ := midxChunk(t, d, midxChunkPackNames)
				d[entry+3] = 'X'
				return d
			})
		},
		"missing lookup": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				entry, _, _ := midxChunk(t, d, midxChunkOIDLookup)
				d[entry+3] = 'X'
				return d
			})
		},
		"fanout order": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, _ := midxChunk(t, d, midxChunkOIDFanout)
				binary.BigEndian.PutUint32(d[start:], 0xffff_ffff)
				return d
			})
		},
		"short pack names": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, end := midxChunk(t, d, midxChunkPackNames)
				for i := start; i < end; i++ {
					if d[i] == 0 {
						d[i] = 'x'
					}
				}
				return d
			})
		},
		"pack names order": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, _ := midxChunk(t, d, midxChunkPackNames)
				first := bytes.IndexByte(d[start:], 0) + 1
				swapped := slices.Concat(d[start+first:start+2*first], d[start:start+first])
				copy(d[start:], swapped)
				return d
			})
		},
		"pack id": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, end := midxChunk(t, d, midxChunkOffsets)
				for i := start; i < end; i += midxOffsetEntrySize {
					binary.BigEndian.PutUint32(d[i:], 99)
				}
				return d
			})
		},
		"offset beyond pack": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, end := midxChunk(t, d, midxChunkOffsets)
				for i := start; i < end; i += midxOffsetEntrySize {
					binary.BigEndian.PutUint32(d[i+4:], 0x7fff_fff0)
				}
				return d
			})
		},
		"large offset without table": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				_, start, end := midxChunk(t, d, midxChunkOffsets)
				for i := start; i < end; i += midxOffsetEntrySize {
					binary.BigEndian.PutUint32(d[i+4:], 0x8000_0000)
				}
				return d
			})
		},
		"missing fanout": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte {
				entry, _, _ := midxChunk(t, d, midxChunkOIDFanout)
				d[entry+3] = 'X'
				return d
			})
		},
		"chunk count": func(t *testing.T, _ *testRepo, midx string) {
			patchFile(t, midx, func(d []byte) []byte { d[6] = 255; return d })
		},
		"index removed": func(t *testing.T, fixture *testRepo, _ string) {
			indexes, err := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.idx"))
			if err != nil || len(indexes) != 2 {
				t.Fatalf("pack indexes %v: %v", indexes, err)
			}
			for _, index := range indexes {
				if err := os.Remove(index); err != nil {
					t.Fatal(err)
				}
			}
		},
		"pack removed": func(t *testing.T, fixture *testRepo, _ string) {
			packs, err := filepath.Glob(filepath.Join(fixture.root, ".git", "objects", "pack", "*.pack"))
			if err != nil || len(packs) != 2 {
				t.Fatalf("packs %v: %v", packs, err)
			}
			for _, pack := range packs {
				if err := os.Remove(pack); err != nil {
					t.Fatal(err)
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := sparseFixture(t)
			fixture.twoPacks(func() {})
			fixture.git("multi-pack-index", "write")
			mutate(t, fixture, filepath.Join(fixture.root, ".git", "objects", "pack", "multi-pack-index"))
			fixture.matchGit()
		})
	}
}

func TestExpandedIndexMultiPackIndexLargeOffsets(t *testing.T) {
	fixture := sparseFixture(t)
	fixture.twoPacks(func() {})
	fixture.git("multi-pack-index", "write")
	midx := filepath.Join(fixture.root, ".git", "objects", "pack", "multi-pack-index")
	data, err := os.ReadFile(midx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := loadMultiPackIndexFile(testDisk(t), midx, 20)
	if err != nil || m == nil {
		t.Fatalf("loadMultiPackIndexFile = %v, %v", m, err)
	}
	_, start, end := midxChunk(t, data, midxChunkOffsets)
	var large []byte
	for i := start; i < end; i += midxOffsetEntrySize {
		large = binary.BigEndian.AppendUint64(large, uint64(binary.BigEndian.Uint32(data[i+4:])))
		binary.BigEndian.PutUint32(data[i+4:], packLargeOffsetFlag|uint32((i-start)/midxOffsetEntrySize))
	}
	m.offsets = data[start:end]
	m.large = large
	tree, err := hex.DecodeString(fixture.treeOID("out/bulk"))
	if err != nil {
		t.Fatal(err)
	}
	if _, offset, ok, err := m.find(tree); !ok || err != nil || offset == 0 || offset&packLargeOffsetFlag != 0 {
		t.Fatalf("find = %d, %v, %v through the large offset table", offset, ok, err)
	}
	m.large = []byte{}
	if _, _, _, err := m.find(tree); err == nil {
		t.Fatal("find accepted a large offset beyond the table")
	}
}

func TestExpandedIndexCorruptMultiPackIndexChainMatchesGit(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, chain string){
		"bad line": func(t *testing.T, chain string) {
			patchFile(t, chain, func(d []byte) []byte { d[0] = 'z'; return d })
		},
		"short line": func(t *testing.T, chain string) {
			patchFile(t, chain, func(d []byte) []byte { return append([]byte("abc\n"), d...) })
		},
		"missing layer": func(t *testing.T, chain string) {
			patchFile(t, chain, func(d []byte) []byte { return append([]byte(strings.Repeat("a", 40)+"\n"), d...) })
		},
		"unterminated": func(t *testing.T, chain string) {
			patchFile(t, chain, func(d []byte) []byte { return bytes.TrimSuffix(d, []byte("\n")) })
		},
		"tiny": func(t *testing.T, chain string) {
			patchFile(t, chain, func([]byte) []byte { return []byte("abc") })
		},
		"corrupt layer": func(t *testing.T, chain string) {
			data, err := os.ReadFile(chain)
			if err != nil {
				t.Fatal(err)
			}
			layer := filepath.Join(filepath.Dir(chain), "multi-pack-index-"+string(data[:40])+".midx")
			patchFile(t, layer, func(d []byte) []byte { d[0] = 'X'; return d })
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := sparseFixture(t)
			fixture.twoPacks(func() { fixture.git("multi-pack-index", "write", "--incremental") })
			fixture.git("multi-pack-index", "write", "--incremental")
			mutate(t, filepath.Join(fixture.root, ".git", "objects", "pack", "multi-pack-index.d", "multi-pack-index-chain"))
			fixture.matchGit()
		})
	}
}

func TestExpandedIndexAlternatesMatchGit(t *testing.T) {
	for hops, reachable := range map[int]bool{5: true, 6: false} {
		t.Run(fmt.Sprintf("%d hops", hops), func(t *testing.T) {
			source := sparseFixture(t)
			source.git("repack", "-a", "-d", "-q")
			source.git("prune-packed")
			clone := &testRepo{t: t, root: filepath.Join(filepath.Dir(source.root), "clone"), home: source.home}
			source.git("clone", "--quiet", "--shared", "--no-checkout", source.root, clone.root)
			clone.git("sparse-checkout", "set", "--cone", "--sparse-index", "in")
			clone.git("checkout", "--quiet", "HEAD")
			target := filepath.Join(source.root, ".git", "objects")
			for i := hops; i >= 1; i-- {
				hop := filepath.Join(filepath.Dir(source.root), fmt.Sprintf("hop%d", i), "objects")
				clone.write(filepath.Join("..", fmt.Sprintf("hop%d", i), "objects", "info", "alternates"), target+"\n")
				target = hop
			}
			clone.write(".git/objects/info/alternates", target+"\n")
			expanded := clone.compareIndex()
			if got := slices.ContainsFunc(expanded, func(e IndexEntry) bool { return e.Path == "out/scripts/run.sh" }); got != reachable {
				t.Fatalf("out/scripts/run.sh expanded = %v, want %v", got, reachable)
			}
		})
	}
	t.Run("not a directory", func(t *testing.T) {
		fixture := sparseFixture(t)
		file := fixture.write("not-objects", "")
		fixture.write(".git/objects/info/alternates", file+"\n")
		fixture.matchGit()
	})
	t.Run("loops", func(t *testing.T) {
		fixture := sparseFixture(t)
		fixture.write(".git/objects/info/alternates", filepath.Join(fixture.root, ".git", "objects")+"\n")
		fixture.matchGit()
	})
}

func TestMidxChunksShortTable(t *testing.T) {
	if chunks, ok := midxChunks(make([]byte, midxHeaderSize+20), 1, 20); ok {
		t.Fatalf("midxChunks = %v for a table past the end of the file", chunks)
	}
}

func TestObjectDirKey(t *testing.T) {
	if got := (&Repository{ignoreCase: true}).objectDirKey("/A/Objects"); got != "/a/objects" {
		t.Fatalf("objectDirKey = %q", got)
	}
	if got := (&Repository{}).objectDirKey("/A/Objects"); got != "/A/Objects" {
		t.Fatalf("objectDirKey = %q", got)
	}
}

func TestPackIndexLargeOffsets(t *testing.T) {
	first, second := bytes.Repeat([]byte{0x10}, 20), bytes.Repeat([]byte{0x11}, 20)
	data := slices.Concat(packIndexSignature, []byte{0, 0, 0, 2})
	for i := range 256 {
		data = binary.BigEndian.AppendUint32(data, uint32(min(max(i-0x0f, 0), 2)))
	}
	data = slices.Concat(data, first, second, make([]byte, 8))
	table := len(data)
	data = binary.BigEndian.AppendUint32(data, packLargeOffsetFlag)
	data = binary.BigEndian.AppendUint32(data, 12)
	data = binary.BigEndian.AppendUint64(data, 1<<33)
	data = append(data, make([]byte, 40)...)
	path := filepath.Join(t.TempDir(), "pack.idx")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	index, err := loadPackIndex(testDisk(t), path, 20)
	if err != nil {
		t.Fatal(err)
	}
	for oid, want := range map[string]uint64{string(first): 1 << 33, string(second): 12} {
		position, ok := index.find([]byte(oid))
		if !ok {
			t.Fatalf("find missed %x", oid)
		}
		if offset, err := index.offset(position); err != nil || offset != want {
			t.Fatalf("offset(%x) = %d, %v, want %d", oid, offset, err, want)
		}
	}
	if _, ok := index.find(bytes.Repeat([]byte{0x20}, 20)); ok {
		t.Fatal("find matched a missing object")
	}
	binary.BigEndian.PutUint32(index.data[table:], packLargeOffsetFlag|4)
	if offset, err := index.offset(0); err != nil || offset != 0 {
		t.Fatalf("offset = %d, %v, want the zero bytes before the last 8 index bytes", offset, err)
	}
	binary.BigEndian.PutUint32(index.data[table:], packLargeOffsetFlag|5)
	if offset, err := index.offset(0); err == nil {
		t.Fatalf("offset = %d, want an error for a large offset past the table", offset)
	}
}

func TestPackInflateRejects(t *testing.T) {
	p := newPackedRepo(t)
	pack := newPackFile(testDisk(t), p.index)
	if err := pack.open(20); err != nil {
		t.Fatal(err)
	}
	defer pack.close()
	header, err := pack.window(p.offset)
	if err != nil {
		t.Fatal(err)
	}
	_, used, err := pack.deltaBase(objectOffsetDelta, header[p.header:], p.offset)
	if err != nil {
		t.Fatal(err)
	}
	data := p.offset + uint64(p.header) + uint64(used)
	for name, tc := range map[string]struct{ offset, size uint64 }{
		"claimed size":   {data, 1 << 62},
		"short stream":   {data, 1 << 20},
		"trailing bytes": {data, 1},
		"not zlib":       {0, 10},
	} {
		if got, err := pack.inflate(tc.offset, tc.size); err == nil {
			t.Errorf("%s: inflate = %q, want an error", name, got)
		}
	}
}

func TestPackDeltaBaseRejects(t *testing.T) {
	pack := &packFile{path: "test.pack", index: &packIndex{hashLength: 20}}
	for name, tc := range map[string]struct {
		kind objectType
		info []byte
	}{
		"short reference":  {objectRefDelta, make([]byte, 10)},
		"truncated offset": {objectOffsetDelta, []byte{0x80}},
		"empty offset":     {objectOffsetDelta, nil},
		"overflow":         {objectOffsetDelta, bytes.Repeat([]byte{0xff}, 12)},
		"zero offset":      {objectOffsetDelta, []byte{0}},
		"before the pack":  {objectOffsetDelta, []byte{0x7f}},
	} {
		if base, _, err := pack.deltaBase(tc.kind, tc.info, 100); err == nil {
			t.Errorf("%s: deltaBase = %d, want an error", name, base)
		}
	}
}

func TestPackWindowAfterTruncation(t *testing.T) {
	p := newPackedRepo(t)
	pack := newPackFile(testDisk(t), p.index)
	if err := pack.open(20); err != nil {
		t.Fatal(err)
	}
	defer pack.close()
	if err := os.Chmod(p.pack, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p.pack, int64(p.offset)); err != nil {
		t.Fatal(err)
	}
	if got, err := pack.window(p.offset); err == nil {
		t.Fatalf("window = %x, want a read error", got)
	}
	if got, err := pack.window(uint64(pack.size)); err == nil {
		t.Fatalf("window = %x, want an error past the end", got)
	}
}

func encodeOffset(relative uint64) []byte {
	out := []byte{byte(relative & 0x7f)}
	for relative >>= 7; relative != 0; relative >>= 7 {
		relative--
		out = append([]byte{0x80 | byte(relative&0x7f)}, out...)
	}
	return out
}

func TestPathRulesFrom(t *testing.T) {
	rules, err := pathRulesFrom(config{"core.protectntfs": {valueOf("false")}, "core.protecthfs": {valueOf("true")}})
	if err != nil || rules.ntfs || !rules.hfs {
		t.Fatalf("pathRulesFrom = %+v, %v", rules, err)
	}
	if _, err := pathRulesFrom(config{"core.protectntfs": {valueOf("maybe")}}); err == nil {
		t.Fatal("pathRulesFrom accepted a bad boolean")
	}
}

package gitrepo

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	objectOffsetDelta objectType = 6
	objectRefDelta    objectType = 7

	packIndexFanoutSize   = 4 * 256
	packLargeOffsetFlag   = 0x8000_0000
	deltaMinimumSize      = 4
	zlibMaximumRatio      = 1032
	packMaximumHashLength = 32
)

var packIndexSignature = []byte{0xff, 't', 'O', 'c'}

type packIndex struct {
	hashLength int
	version    int
	data       []byte
	count      uint32
}

func loadPackIndex(d disk, path string, hashLength int) (*packIndex, error) {
	data, err := d.readFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < packIndexFanoutSize+2*hashLength {
		return nil, fmt.Errorf("index file %s is too small", path)
	}
	index := &packIndex{hashLength: hashLength, version: 1, data: data}
	fanout := data
	if bytes.HasPrefix(data, packIndexSignature) {
		if version := binary.BigEndian.Uint32(data[4:]); version != 2 {
			return nil, fmt.Errorf("index file %s is version %d", path, version)
		}
		index.version = 2
		fanout = data[8:]
	}
	for i := range 256 {
		n := binary.BigEndian.Uint32(fanout[i*4:])
		if n < index.count {
			return nil, fmt.Errorf("non-monotonic index %s", path)
		}
		index.count = n
	}
	count, hash := uint64(index.count), uint64(hashLength)
	if index.version == 1 {
		if uint64(len(data)) != packIndexFanoutSize+2*hash+count*(hash+4) {
			return nil, fmt.Errorf("wrong index v1 file size in %s", path)
		}
		return index, nil
	}
	minimum := 8 + packIndexFanoutSize + 2*hash + count*(hash+8)
	maximum := minimum
	if count > 0 {
		maximum += (count - 1) * 8
	}
	if size := uint64(len(data)); size < minimum || size > maximum {
		return nil, fmt.Errorf("wrong index v2 file size in %s", path)
	}
	return index, nil
}

func (x *packIndex) fanout(i int) uint32 {
	start := 0
	if x.version == 2 {
		start = 8
	}
	return binary.BigEndian.Uint32(x.data[start+i*4:])
}

func (x *packIndex) name(n uint32) []byte {
	if x.version == 1 {
		start := packIndexFanoutSize + int(n)*(x.hashLength+4) + 4
		return x.data[start : start+x.hashLength]
	}
	start := 8 + packIndexFanoutSize + int(n)*x.hashLength
	return x.data[start : start+x.hashLength]
}

func (x *packIndex) find(oid []byte) (uint32, bool) {
	return searchFanout(oid, x.fanout, x.name)
}

func searchFanout(oid []byte, fanout func(int) uint32, name func(uint32) []byte) (uint32, bool) {
	low := uint32(0)
	if oid[0] > 0 {
		low = fanout(int(oid[0]) - 1)
	}
	high := fanout(int(oid[0]))
	for low < high {
		middle := low + (high-low)/2
		switch bytes.Compare(oid, name(middle)) {
		case 0:
			return middle, true
		case -1:
			high = middle
		default:
			low = middle + 1
		}
	}
	return 0, false
}

func (x *packIndex) offset(n uint32) (uint64, error) {
	if x.version == 1 {
		return uint64(binary.BigEndian.Uint32(x.data[packIndexFanoutSize+int(n)*(x.hashLength+4):])), nil
	}
	table := 8 + packIndexFanoutSize + int(x.count)*(x.hashLength+4)
	offset := binary.BigEndian.Uint32(x.data[table+int(n)*4:])
	if offset&packLargeOffsetFlag == 0 {
		return uint64(offset), nil
	}
	large := uint64(table) + uint64(x.count)*4 + uint64(offset&^packLargeOffsetFlag)*8
	if large >= uint64(len(x.data)-8) {
		return 0, errors.New("offset beyond end of pack index (truncated index?)")
	}
	return binary.BigEndian.Uint64(x.data[large:]), nil
}

func (x *packIndex) packChecksum() []byte {
	return x.data[len(x.data)-2*x.hashLength : len(x.data)-x.hashLength]
}

type packFile struct {
	disk   disk
	path   string
	index  *packIndex
	file   *os.File
	size   int64
	opened bool
	err    error
}

func newPackFile(d disk, indexPath string) *packFile {
	return &packFile{disk: d, path: strings.TrimSuffix(indexPath, ".idx") + ".pack"}
}

func (p *packFile) indexPath() string {
	return strings.TrimSuffix(p.path, ".pack") + ".idx"
}

func (p *packFile) open(hashLength int) error {
	if p.opened {
		return p.err
	}
	p.opened = true
	p.err = p.validate(hashLength)
	return p.err
}

func (p *packFile) validate(hashLength int) error {
	index, err := loadPackIndex(p.disk, p.indexPath(), hashLength)
	if err != nil {
		return fmt.Errorf("packfile %s index unavailable: %w", p.path, err)
	}
	p.index = index
	if p.file, err = p.disk.open(p.path); err != nil {
		return err
	}
	info, err := p.file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("packfile %s not a regular file", p.path)
	}
	p.size = info.Size()
	header := make([]byte, 12)
	if _, err := p.file.ReadAt(header, 0); err != nil {
		return fmt.Errorf("file %s is far too short to be a packfile", p.path)
	}
	if string(header[:4]) != "PACK" {
		return fmt.Errorf("file %s is not a GIT packfile", p.path)
	}
	if version := binary.BigEndian.Uint32(header[4:]); version != 2 && version != 3 {
		return fmt.Errorf("packfile %s is version %d", p.path, version)
	}
	if count := binary.BigEndian.Uint32(header[8:]); count != index.count {
		return fmt.Errorf("packfile %s claims to have %d objects while index indicates %d objects", p.path, count, index.count)
	}
	checksum := make([]byte, hashLength)
	if _, err := p.file.ReadAt(checksum, p.size-int64(hashLength)); err != nil || p.size < int64(len(header)+hashLength) {
		return fmt.Errorf("packfile %s signature is unavailable", p.path)
	}
	if !bytes.Equal(checksum, index.packChecksum()) {
		return fmt.Errorf("packfile %s does not match index", p.path)
	}
	return nil
}

func (p *packFile) close() {
	if p.file != nil {
		_ = p.file.Close()
	}
}

func (p *packFile) window(offset uint64) ([]byte, error) {
	limit := uint64(p.size) - uint64(p.index.hashLength)
	if offset > limit {
		return nil, fmt.Errorf("offset beyond end of packfile %s (truncated pack?)", p.path)
	}
	buf := make([]byte, min(limit-offset, 16+packMaximumHashLength))
	if _, err := p.file.ReadAt(buf, int64(offset)); err != nil {
		return nil, err
	}
	return buf, nil
}

type packDelta struct {
	data uint64
	size uint64
}

func (p *packFile) read(offset uint64) (objectType, []byte, error) {
	var deltas []packDelta
	for {
		header, err := p.window(offset)
		if err != nil {
			return 0, nil, err
		}
		kind, size, used, err := packObjectHeader(header)
		if err != nil {
			return 0, nil, fmt.Errorf("%s at offset %d: %w", p.path, offset, err)
		}
		position := offset + uint64(used)
		switch kind {
		case objectCommit, objectTree, objectBlob, objectTag:
			data, err := p.inflate(position, size)
			if err != nil {
				return 0, nil, err
			}
			for _, step := range slices.Backward(deltas) {
				delta, err := p.inflate(step.data, step.size)
				if err != nil {
					return 0, nil, err
				}
				if data, err = applyDelta(data, delta); err != nil {
					return 0, nil, fmt.Errorf("%s: %w", p.path, err)
				}
			}
			return kind, data, nil
		case objectOffsetDelta, objectRefDelta:
		default:
			return 0, nil, fmt.Errorf("unknown object type %d at offset %d in %s", kind, offset, p.path)
		}
		base, used, err := p.deltaBase(kind, header[used:], offset)
		if err != nil {
			return 0, nil, err
		}
		deltas = append(deltas, packDelta{data: position + uint64(used), size: size})
		if uint64(len(deltas)) > uint64(p.index.count) {
			return 0, nil, fmt.Errorf("delta chain at offset %d in %s loops", offset, p.path)
		}
		offset = base
	}
}

func (p *packFile) deltaBase(kind objectType, info []byte, offset uint64) (uint64, int, error) {
	if kind == objectRefDelta {
		if len(info) < p.index.hashLength {
			return 0, 0, fmt.Errorf("offset beyond end of packfile %s (truncated pack?)", p.path)
		}
		oid := info[:p.index.hashLength]
		position, ok := p.index.find(oid)
		if !ok {
			return 0, 0, fmt.Errorf("delta base %s of the object at offset %d is missing from %s", hex.EncodeToString(oid), offset, p.path)
		}
		base, err := p.index.offset(position)
		return base, p.index.hashLength, err
	}
	used := 0
	next := func() (byte, bool) {
		if used >= len(info) {
			return 0, false
		}
		used++
		return info[used-1], true
	}
	c, ok := next()
	relative := uint64(c & 0x7f)
	for ok && c&0x80 != 0 {
		relative++
		if relative == 0 || relative>>(64-7) != 0 {
			ok = false
			break
		}
		if c, ok = next(); ok {
			relative = relative<<7 + uint64(c&0x7f)
		}
	}
	if !ok || relative == 0 || relative >= offset {
		return 0, 0, fmt.Errorf("failed to validate delta base reference at offset %d from %s", offset, p.path)
	}
	return offset - relative, used, nil
}

func (p *packFile) inflate(offset, size uint64) ([]byte, error) {
	compressed := uint64(p.size) - offset
	z, err := newInflater(io.NewSectionReader(p.file, int64(offset), int64(compressed)), compressed)
	if err != nil {
		return nil, fmt.Errorf("inflate at offset %d in %s: %w", offset, p.path, err)
	}
	data, err := z.exact(size)
	if err != nil {
		return nil, fmt.Errorf("inflate at offset %d in %s: %w", offset, p.path, err)
	}
	return data, nil
}

func packObjectHeader(buf []byte) (objectType, uint64, int, error) {
	if len(buf) == 0 {
		return 0, 0, 0, errors.New("bad object header")
	}
	c := buf[0]
	used := 1
	size := uint64(c & 15)
	for shift := 4; c&0x80 != 0; shift += 7 {
		if used >= len(buf) || shift > 64-7 {
			return 0, 0, 0, errors.New("bad object header")
		}
		c = buf[used]
		used++
		size |= uint64(c&0x7f) << shift
	}
	return objectType(buf[0] >> 4 & 7), size, used, nil
}

func deltaHeaderSize(data []byte, pos *int) (uint64, error) {
	var size uint64
	for shift := 0; ; shift += 7 {
		if *pos >= len(data) {
			return 0, errors.New("delta replay has gone wild")
		}
		c := data[*pos]
		*pos++
		if shift > 63 || (shift > 0 && uint64(c&0x7f)>>(64-shift) != 0) {
			return 0, errors.New("delta header size overflows")
		}
		size |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return size, nil
		}
	}
}

func applyDelta(base, delta []byte) ([]byte, error) {
	if len(delta) < deltaMinimumSize {
		return nil, errors.New("the delta is too short")
	}
	pos := 0
	sourceSize, err := deltaHeaderSize(delta, &pos)
	if err != nil {
		return nil, err
	}
	if sourceSize != uint64(len(base)) {
		return nil, errors.New("the delta base size does not match")
	}
	size, err := deltaHeaderSize(delta, &pos)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, min(size, uint64(len(base))+uint64(len(delta))))
	remaining := size
	for pos < len(delta) {
		cmd := delta[pos]
		pos++
		switch {
		case cmd&0x80 != 0:
			var copyOffset, copySize uint64
			for bit, shift := range []uint{0, 8, 16, 24, 0, 8, 16} {
				if cmd&(1<<bit) == 0 {
					continue
				}
				if pos >= len(delta) {
					return nil, errors.New("delta replay has gone wild")
				}
				if bit < 4 {
					copyOffset |= uint64(delta[pos]) << shift
				} else {
					copySize |= uint64(delta[pos]) << shift
				}
				pos++
			}
			if copySize == 0 {
				copySize = 0x10000
			}
			if copyOffset+copySize > uint64(len(base)) || copySize > remaining {
				return nil, errors.New("delta replay has gone wild")
			}
			out = append(out, base[copyOffset:copyOffset+copySize]...)
			remaining -= copySize
		case cmd != 0:
			if uint64(cmd) > remaining || int(cmd) > len(delta)-pos {
				return nil, errors.New("delta replay has gone wild")
			}
			out = append(out, delta[pos:pos+int(cmd)]...)
			pos += int(cmd)
			remaining -= uint64(cmd)
		default:
			return nil, errors.New("unexpected delta opcode 0")
		}
	}
	if remaining != 0 {
		return nil, errors.New("delta replay has gone wild")
	}
	return out, nil
}

const (
	midxHeaderSize      = 12
	midxChunkEntrySize  = 12
	midxChunkPackNames  = "PNAM"
	midxChunkOIDFanout  = "OIDF"
	midxChunkOIDLookup  = "OIDL"
	midxChunkOffsets    = "OOFF"
	midxChunkLarge      = "LOFF"
	midxOffsetEntrySize = 8
)

type multiPackIndex struct {
	hashLength  int
	packs       []string
	fanoutChunk []byte
	lookup      []byte
	offsets     []byte
	large       []byte
	count       uint32
	base        *multiPackIndex
}

func loadMultiPackIndex(d disk, packDir string, hashLength int) (*multiPackIndex, error) {
	m, err := loadMultiPackIndexFile(d, filepath.Join(packDir, "multi-pack-index"), hashLength)
	if m != nil || err != nil {
		return m, err
	}
	chainDir := filepath.Join(packDir, "multi-pack-index.d")
	chain, err := d.readOptional(filepath.Join(chainDir, "multi-pack-index-chain"))
	if err != nil || len(chain) < hashLength*2 {
		return nil, err
	}
	var top *multiPackIndex
	lines := strings.Split(string(chain), "\n")
	for _, line := range lines[:min(len(lines), len(chain)/(hashLength*2+1))] {
		if len(line) != hashLength*2 || strings.Trim(line, "0123456789abcdefABCDEF") != "" {
			break
		}
		layer, err := loadMultiPackIndexFile(d, filepath.Join(chainDir, "multi-pack-index-"+line+".midx"), hashLength)
		if err != nil {
			return nil, err
		}
		if layer == nil {
			break
		}
		layer.base = top
		top = layer
	}
	return top, nil
}

func loadMultiPackIndexFile(d disk, path string, hashLength int) (*multiPackIndex, error) {
	data, err := d.readOptional(path)
	if err != nil || data == nil || len(data) < midxHeaderSize+hashLength {
		return nil, err
	}
	if string(data[:4]) != "MIDX" {
		return nil, fmt.Errorf("multi-pack-index signature 0x%08x does not match signature 0x%08x", binary.BigEndian.Uint32(data), binary.BigEndian.Uint32([]byte("MIDX")))
	}
	if version := data[4]; version != 1 && version != 2 {
		return nil, fmt.Errorf("multi-pack-index version %d not recognized", version)
	}
	wantHash := byte(1)
	if hashLength == 32 {
		wantHash = 2
	}
	if data[5] != wantHash {
		return nil, nil
	}
	chunks, ok := midxChunks(data, int(data[6]), hashLength)
	if !ok {
		return nil, nil
	}
	m := &multiPackIndex{hashLength: hashLength}
	names, ok := chunks[midxChunkPackNames]
	if !ok {
		return nil, errors.New("multi-pack-index required pack-name chunk missing or corrupted")
	}
	if m.fanoutChunk, ok = chunks[midxChunkOIDFanout]; !ok || len(m.fanoutChunk) != 4*256 {
		return nil, errors.New("multi-pack-index required OID fanout chunk missing or corrupted")
	}
	for i := range 255 {
		if m.fanout(i) > m.fanout(i+1) {
			return nil, errors.New("multi-pack-index required OID fanout chunk missing or corrupted")
		}
	}
	m.count = m.fanout(255)
	if m.lookup, ok = chunks[midxChunkOIDLookup]; !ok || uint64(len(m.lookup)) != uint64(m.count)*uint64(hashLength) {
		return nil, errors.New("multi-pack-index required OID lookup chunk missing or corrupted")
	}
	if m.offsets, ok = chunks[midxChunkOffsets]; !ok || uint64(len(m.offsets)) != uint64(m.count)*midxOffsetEntrySize {
		return nil, errors.New("multi-pack-index required object offsets chunk missing or corrupted")
	}
	m.large = chunks[midxChunkLarge]
	packCount := binary.BigEndian.Uint32(data[8:])
	for i := range packCount {
		end := bytes.IndexByte(names, 0)
		if end < 0 {
			return nil, errors.New("multi-pack-index pack-name chunk is too short")
		}
		name := string(names[:end])
		if data[4] == 1 && i > 0 && name <= m.packs[i-1] {
			return nil, fmt.Errorf("multi-pack-index pack names out of order: %q before %q", m.packs[i-1], name)
		}
		m.packs = append(m.packs, name)
		names = names[end+1:]
	}
	return m, nil
}

func midxChunks(data []byte, count, hashLength int) (map[string][]byte, bool) {
	chunks := make(map[string][]byte)
	toc := data[midxHeaderSize:]
	if len(toc) < (count+1)*midxChunkEntrySize {
		return nil, false
	}
	limit := uint64(len(data) - hashLength)
	for i := range count {
		entry := toc[i*midxChunkEntrySize:]
		id := string(entry[:4])
		start := binary.BigEndian.Uint64(entry[4:])
		end := binary.BigEndian.Uint64(entry[midxChunkEntrySize+4:])
		if binary.BigEndian.Uint32(entry) == 0 || start%4 != 0 || end < start || end > limit {
			return nil, false
		}
		if _, duplicate := chunks[id]; duplicate {
			return nil, false
		}
		chunks[id] = data[start:end]
	}
	return chunks, binary.BigEndian.Uint32(toc[count*midxChunkEntrySize:]) == 0
}

func (m *multiPackIndex) fanout(i int) uint32 {
	return binary.BigEndian.Uint32(m.fanoutChunk[i*4:])
}

func (m *multiPackIndex) name(n uint32) []byte {
	return m.lookup[int(n)*m.hashLength : int(n+1)*m.hashLength]
}

func (m *multiPackIndex) find(oid []byte) (string, uint64, bool, error) {
	for layer := m; layer != nil; layer = layer.base {
		position, ok := searchFanout(oid, layer.fanout, layer.name)
		if !ok {
			continue
		}
		entry := layer.offsets[int(position)*midxOffsetEntrySize:]
		pack := binary.BigEndian.Uint32(entry)
		if pack >= uint32(len(layer.packs)) {
			return "", 0, false, fmt.Errorf("bad pack-int-id: %d (%d total packs)", pack, len(layer.packs))
		}
		offset := binary.BigEndian.Uint32(entry[4:])
		if layer.large == nil || offset&packLargeOffsetFlag == 0 {
			return layer.packs[pack], uint64(offset), true, nil
		}
		large := uint64(offset &^ packLargeOffsetFlag)
		if large >= uint64(len(layer.large)/8) {
			return "", 0, false, errors.New("multi-pack-index large offset out of bounds")
		}
		return layer.packs[pack], binary.BigEndian.Uint64(layer.large[large*8:]), true, nil
	}
	return "", 0, false, nil
}

func (m *multiPackIndex) contains(indexName string) bool {
	for layer := m; layer != nil; layer = layer.base {
		for _, name := range layer.packs {
			if name == indexName || strings.TrimSuffix(name, ".idx")+".pack" == indexName {
				return true
			}
		}
	}
	return false
}

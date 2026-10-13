package gitrepo

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

const (
	indexStatBeforeMode = 24
	indexStatAfterMode  = 12
	indexExtendedFlag   = 0x4000
	indexNameLengthMask = 0xfff
)

type indexEntry struct {
	path    []byte
	tracked bool
}

type indexFile struct {
	entries []indexEntry
	link    *indexLink
}

type indexLink struct {
	shared  string
	bitmaps []byte
}

func (r *Repository) TrackedFiles() ([]string, error) {
	path := filepath.Join(r.GitDir, "index")
	data, err := readOptional(path)
	if err != nil || data == nil {
		return nil, err
	}
	paths, err := indexPaths(data, r.hashLength, func(shared string) ([]byte, error) {
		data, err := readOptional(filepath.Join(r.GitDir, "sharedindex."+shared))
		if err == nil && data == nil {
			err = errors.New("the shared index is missing")
		}
		return data, err
	})
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	return paths, nil
}

func indexPaths(data []byte, hashLength int, loadShared func(shared string) ([]byte, error)) ([]string, error) {
	index, err := parseIndex(data, hashLength)
	if err != nil {
		return nil, err
	}
	entries := index.entries
	if index.link != nil {
		shared, err := loadShared(index.link.shared)
		if err != nil {
			return nil, err
		}
		base, err := parseIndex(shared, hashLength)
		if err != nil {
			return nil, fmt.Errorf("shared index %s: %w", index.link.shared, err)
		}
		if base.link != nil {
			return nil, fmt.Errorf("shared index %s is split itself", index.link.shared)
		}
		if entries, err = mergeSplitIndex(base.entries, index.entries, index.link.bitmaps); err != nil {
			return nil, err
		}
	}
	var paths []string
	for _, entry := range entries {
		if len(entry.path) == 0 {
			return nil, errors.New("an index entry has an empty path")
		}
		if entry.tracked {
			paths = append(paths, string(entry.path))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

func mergeSplitIndex(base, split []indexEntry, bitmaps []byte) ([]indexEntry, error) {
	reader := indexReader{data: bitmaps}
	deletedPositions, err := reader.ewah(len(base))
	if err != nil {
		return nil, err
	}
	replaced, err := reader.ewah(len(base))
	if err != nil {
		return nil, err
	}
	if !reader.done() {
		return nil, errors.New("the link extension has trailing data")
	}
	if len(replaced) > len(split) {
		return nil, fmt.Errorf("the split index replaces %d entries but holds %d", len(replaced), len(split))
	}
	deleted := make(map[int]bool, len(deletedPositions))
	for _, position := range deletedPositions {
		deleted[position] = true
	}
	merged := slices.Clone(base)
	for i, position := range replaced {
		merged[position] = indexEntry{path: base[position].path, tracked: split[i].tracked}
	}
	var entries []indexEntry
	for position, entry := range merged {
		if !deleted[position] {
			entries = append(entries, entry)
		}
	}
	return append(entries, split[len(replaced):]...), nil
}

func parseIndex(data []byte, hashLength int) (*indexFile, error) {
	if len(data) < hashLength {
		return nil, errors.New("the index is shorter than its checksum")
	}
	reader := indexReader{data: data[:len(data)-hashLength]}
	signature, err := reader.bytes(4)
	if err != nil {
		return nil, err
	}
	if string(signature) != "DIRC" {
		return nil, errors.New("the index does not start with DIRC")
	}
	version, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("unsupported index version %d", version)
	}
	count, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	index := &indexFile{}
	var previous []byte
	for range count {
		entry, err := reader.entry(version, hashLength, previous)
		if err != nil {
			return nil, err
		}
		previous = entry.path
		index.entries = append(index.entries, entry)
	}
	for !reader.done() {
		signature, err := reader.bytes(4)
		if err != nil {
			return nil, err
		}
		size, err := reader.uint32()
		if err != nil {
			return nil, err
		}
		payload, err := reader.bytes(int(size))
		if err != nil {
			return nil, err
		}
		switch {
		case string(signature) == "link":
			if index.link != nil {
				return nil, errors.New("the index has two link extensions")
			}
			if len(payload) < hashLength {
				return nil, errors.New("the link extension is shorter than its shared index hash")
			}
			index.link = &indexLink{shared: hex.EncodeToString(payload[:hashLength]), bitmaps: payload[hashLength:]}
		case string(signature) == "sdir":
		case signature[0] >= 'A' && signature[0] <= 'Z':
		default:
			return nil, fmt.Errorf("the index uses the %s extension", signature)
		}
	}
	return index, nil
}

type indexReader struct {
	data []byte
	pos  int
}

func (r *indexReader) done() bool { return r.pos >= len(r.data) }

func (r *indexReader) bytes(n int) ([]byte, error) {
	if n < 0 || len(r.data)-r.pos < n {
		return nil, fmt.Errorf("the index ends after %d bytes", len(r.data))
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *indexReader) uint32() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *indexReader) uint16() (uint16, error) {
	b, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *indexReader) nulTerminated() ([]byte, error) {
	end := bytes.IndexByte(r.data[r.pos:], 0)
	if end < 0 {
		return nil, errors.New("the index has an unterminated name")
	}
	out := r.data[r.pos : r.pos+end]
	r.pos += end + 1
	return out, nil
}

func (r *indexReader) varint() (int, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	value := int(b[0] & 0x7f)
	for b[0]&0x80 != 0 {
		if b, err = r.bytes(1); err != nil {
			return 0, err
		}
		value = (value+1)<<7 | int(b[0]&0x7f)
	}
	return value, nil
}

func (r *indexReader) entry(version uint32, hashLength int, previous []byte) (indexEntry, error) {
	start := r.pos
	if _, err := r.bytes(indexStatBeforeMode); err != nil {
		return indexEntry{}, err
	}
	mode, err := r.uint32()
	if err != nil {
		return indexEntry{}, err
	}
	if _, err := r.bytes(indexStatAfterMode + hashLength); err != nil {
		return indexEntry{}, err
	}
	flags, err := r.uint16()
	if err != nil {
		return indexEntry{}, err
	}
	if flags&indexExtendedFlag != 0 {
		if _, err := r.uint16(); err != nil {
			return indexEntry{}, err
		}
	}
	var path []byte
	if version == 4 {
		strip, err := r.varint()
		if err != nil {
			return indexEntry{}, err
		}
		suffix, err := r.nulTerminated()
		if err != nil {
			return indexEntry{}, err
		}
		if strip > len(previous) {
			return indexEntry{}, fmt.Errorf("an index entry removes %d bytes from a %d-byte path", strip, len(previous))
		}
		path = append(append([]byte{}, previous[:len(previous)-strip]...), suffix...)
	} else {
		name, err := r.nulTerminated()
		if err != nil {
			return indexEntry{}, err
		}
		length := r.pos - start
		if _, err := r.bytes((length+7)/8*8 - length); err != nil {
			return indexEntry{}, err
		}
		path = name
	}
	nameLength := int(flags & indexNameLengthMask)
	if (nameLength < indexNameLengthMask && len(path) != nameLength) || len(path) < nameLength {
		return indexEntry{}, fmt.Errorf("the index entry %q does not match its length %d", path, nameLength)
	}
	switch mode >> 12 {
	case 0b1000, 0b1010:
		return indexEntry{path: path, tracked: true}, nil
	case 0b1110, 0b0100:
		return indexEntry{path: path}, nil
	}
	return indexEntry{}, fmt.Errorf("the index entry %q has mode %o", path, mode)
}

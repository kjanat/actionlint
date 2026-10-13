package gitrepo

import (
	"bytes"
	"encoding/binary"
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

func (r *Repository) TrackedFiles() ([]string, error) {
	data, err := readOptional(filepath.Join(r.GitDir, "index"))
	if err != nil || data == nil {
		return nil, err
	}
	paths, err := indexPaths(data, r.hashLength)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", filepath.Join(r.GitDir, "index"), err)
	}
	return paths, nil
}

func indexPaths(data []byte, hashLength int) ([]string, error) {
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
	var paths []string
	var previous []byte
	for range count {
		path, tracked, err := reader.entry(version, hashLength, previous)
		if err != nil {
			return nil, err
		}
		previous = path
		if tracked {
			paths = append(paths, string(path))
		}
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
		if _, err := reader.bytes(int(size)); err != nil {
			return nil, err
		}
		if signature[0] < 'A' || signature[0] > 'Z' {
			return nil, fmt.Errorf("the index uses the %s extension", signature)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
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

func (r *indexReader) entry(version uint32, hashLength int, previous []byte) ([]byte, bool, error) {
	start := r.pos
	if _, err := r.bytes(indexStatBeforeMode); err != nil {
		return nil, false, err
	}
	mode, err := r.uint32()
	if err != nil {
		return nil, false, err
	}
	if _, err := r.bytes(indexStatAfterMode + hashLength); err != nil {
		return nil, false, err
	}
	flags, err := r.uint16()
	if err != nil {
		return nil, false, err
	}
	if flags&indexExtendedFlag != 0 {
		if _, err := r.uint16(); err != nil {
			return nil, false, err
		}
	}
	var path []byte
	if version == 4 {
		strip, err := r.varint()
		if err != nil {
			return nil, false, err
		}
		suffix, err := r.nulTerminated()
		if err != nil {
			return nil, false, err
		}
		if strip > len(previous) {
			return nil, false, fmt.Errorf("an index entry removes %d bytes from a %d-byte path", strip, len(previous))
		}
		path = append(append([]byte{}, previous[:len(previous)-strip]...), suffix...)
	} else {
		name, err := r.nulTerminated()
		if err != nil {
			return nil, false, err
		}
		length := r.pos - start
		if _, err := r.bytes((length+7)/8*8 - length); err != nil {
			return nil, false, err
		}
		path = name
	}
	nameLength := int(flags & indexNameLengthMask)
	if len(path) == 0 || (nameLength < indexNameLengthMask && len(path) != nameLength) || len(path) < nameLength {
		return nil, false, fmt.Errorf("the index entry %q does not match its length %d", path, nameLength)
	}
	switch mode >> 12 {
	case 0b1000, 0b1010:
		return path, true, nil
	case 0b1110:
		return path, false, nil
	}
	return nil, false, fmt.Errorf("the index entry %q has mode %o", path, mode)
}

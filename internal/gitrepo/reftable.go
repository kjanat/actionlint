package gitrepo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	reftableHeaderV1   = 24
	reftableHeaderV2   = 28
	reftableFooterSize = 44
	reftableSymref     = 3
)

func (r *Repository) readReftableRef(d disk, name string) (refValue, error) {
	dir, key := r.reftableStack(name)
	tables, err := newReftableLoader(d).load(dir)
	if err != nil {
		return refValue{}, err
	}
	for _, table := range slices.Backward(tables) {
		value, present, err := reftableRef(table.data, key)
		if err != nil {
			return refValue{}, fmt.Errorf("%s: %w", table.path, err)
		}
		if present {
			return value, nil
		}
	}
	return refValue{}, nil
}

func (r *Repository) reftableStack(name string) (string, string) {
	if rest, ok := strings.CutPrefix(name, "worktrees/"); ok {
		worktree, bare, hasSlash := strings.Cut(rest, "/")
		if !hasSlash || currentWorktreeRef(bare) {
			return filepath.Join(r.CommonDir, "worktrees", worktree, "reftable"), bare
		}
	}
	if rest, ok := strings.CutPrefix(name, "main-worktree/"); ok && currentWorktreeRef(rest) {
		return filepath.Join(r.CommonDir, "reftable"), rest
	}
	if currentWorktreeRef(name) && r.GitDir != r.CommonDir {
		return filepath.Join(r.GitDir, "reftable"), name
	}
	return filepath.Join(r.CommonDir, "reftable"), name
}

type reftableTable struct {
	path string
	data []byte
}

type reftableLoader struct {
	readFile func(string) ([]byte, error)
	now      func() time.Time
	wait     func(time.Duration) error
}

func newReftableLoader(d disk) reftableLoader {
	return reftableLoader{readFile: d.readFile, now: time.Now, wait: d.wait}
}

func (l reftableLoader) load(dir string) ([]reftableTable, error) {
	list := filepath.Join(dir, "tables.list")
	deadline := l.now().Add(3 * time.Second)
	var delay time.Duration
	var missing error
	for tries := 1; ; tries++ {
		if tries > 3 && !l.now().Before(deadline) {
			return nil, missing
		}
		names, err := l.names(list)
		if err != nil {
			return nil, err
		}
		tables, err := l.open(dir, names)
		if err == nil {
			return tables, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		missing = err
		after, err := l.names(list)
		if err != nil {
			return nil, err
		}
		if slices.Equal(after, names) {
			return nil, missing
		}
		delay += time.Duration(rand.Int64N(int64(delay)+1)) + time.Millisecond
		if err := l.wait(delay); err != nil {
			return nil, err
		}
	}
}

func (l reftableLoader) names(list string) ([]string, error) {
	data, err := l.readFile(list)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range strings.SplitSeq(string(data), "\n") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func (l reftableLoader) open(dir string, names []string) ([]reftableTable, error) {
	tables := make([]reftableTable, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := l.readFile(path)
		if err != nil {
			return nil, err
		}
		if _, err := reftableLayout(data); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		tables = append(tables, reftableTable{path: path, data: data})
	}
	return tables, nil
}

type reftableShape struct {
	header     int
	hashLength int
	blockSize  int
	refEnd     int
}

func reftableLayout(data []byte) (reftableShape, error) {
	if len(data) < reftableHeaderV1 || string(data[:4]) != "REFT" {
		return reftableShape{}, errors.New("not a reftable")
	}
	shape := reftableShape{header: reftableHeaderV1, hashLength: 20, blockSize: uint24(data[5:])}
	switch data[4] {
	case 1:
	case 2:
		shape.header = reftableHeaderV2
	default:
		return reftableShape{}, fmt.Errorf("unsupported reftable version %d", data[4])
	}
	if shape.header == reftableHeaderV2 && string(data[reftableHeaderV1:reftableHeaderV2]) == "s256" {
		shape.hashLength = 32
	}
	end, err := reftableRefSectionEnd(data, shape.header)
	if err != nil {
		return reftableShape{}, err
	}
	shape.refEnd = end
	return shape, nil
}

func reftableRef(data []byte, name string) (refValue, bool, error) {
	shape, err := reftableLayout(data)
	if err != nil {
		return refValue{}, false, err
	}
	for offset, start := 0, shape.header; start+4 <= shape.refEnd; {
		if data[start] != 'r' {
			return refValue{}, false, nil
		}
		end := offset + uint24(data[start+1:])
		if end < start+4+2 || end > shape.refEnd || shape.blockSize > 0 && end > offset+shape.blockSize {
			return refValue{}, false, errors.New("ref block length out of range")
		}
		value, present, passed, err := reftableBlockRef(data[start+4:end], shape.hashLength, name)
		if err != nil || present || passed {
			return value, present, err
		}
		if shape.blockSize > 0 {
			offset += shape.blockSize
		} else {
			offset = end
		}
		start = offset
	}
	return refValue{}, false, nil
}

func reftableRefSectionEnd(data []byte, header int) (int, error) {
	size := header + reftableFooterSize
	if len(data) < header+size {
		return 0, errors.New("truncated reftable")
	}
	footer := data[len(data)-size:]
	if string(footer[:header]) != string(data[:header]) || crc32.ChecksumIEEE(footer[:size-4]) != binary.BigEndian.Uint32(footer[size-4:]) {
		return 0, errors.New("bad reftable footer")
	}
	end := len(data) - size
	for _, position := range []uint64{binary.BigEndian.Uint64(footer[header:]), binary.BigEndian.Uint64(footer[header+8:]) >> 5, binary.BigEndian.Uint64(footer[header+24:])} {
		if position != 0 && position < uint64(end) {
			end = int(position)
		}
	}
	return end, nil
}

func reftableBlockRef(block []byte, hashLength int, name string) (refValue, bool, bool, error) {
	restarts := int(binary.BigEndian.Uint16(block[len(block)-2:]))
	end := len(block) - 2 - 3*restarts
	if restarts == 0 || end < 0 {
		return refValue{}, false, false, errors.New("ref block restart table out of range")
	}
	var key []byte
	for pos := 0; pos < end; {
		prefix, n, ok := reftableVarint(block[pos:end])
		if !ok {
			return refValue{}, false, false, errors.New("bad ref record")
		}
		pos += n
		packed, n, ok := reftableVarint(block[pos:end])
		if !ok {
			return refValue{}, false, false, errors.New("bad ref record")
		}
		pos += n
		suffix, kind := packed>>3, packed&7
		if prefix > uint64(len(key)) || suffix > uint64(end-pos) {
			return refValue{}, false, false, errors.New("bad ref record")
		}
		key = append(key[:prefix], block[pos:pos+int(suffix)]...)
		pos += int(suffix)
		if _, n, ok = reftableVarint(block[pos:end]); !ok {
			return refValue{}, false, false, errors.New("bad ref record")
		}
		pos += n
		value := refValue{found: true}
		switch kind {
		case 0:
			value.found = false
		case 1:
			pos += hashLength
		case 2:
			pos += 2 * hashLength
		case reftableSymref:
			length, n, ok := reftableVarint(block[pos:end])
			if !ok || length > uint64(end-pos-n) {
				return refValue{}, false, false, errors.New("bad symref record")
			}
			value.target, value.symref = string(block[pos+n:pos+n+int(length)]), true
			pos += n + int(length)
		default:
			return refValue{}, false, false, fmt.Errorf("unknown ref record type %d", kind)
		}
		if pos > end {
			return refValue{}, false, false, errors.New("bad ref record")
		}
		switch strings.Compare(string(key), name) {
		case 0:
			return value, true, false, nil
		case 1:
			return refValue{}, false, true, nil
		}
	}
	return refValue{}, false, false, nil
}

func uint24(data []byte) int {
	return int(data[0])<<16 | int(data[1])<<8 | int(data[2])
}

func reftableVarint(data []byte) (uint64, int, bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	value := uint64(data[0] & 0x7f)
	n := 1
	for data[n-1]&0x80 != 0 {
		if n >= len(data) || n > 9 {
			return 0, 0, false
		}
		value = (value+1)<<7 | uint64(data[n]&0x7f)
		n++
	}
	return value, n, true
}

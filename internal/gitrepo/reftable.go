package gitrepo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	reftableHeaderV1   = 24
	reftableHeaderV2   = 28
	reftableFooterSize = 44
	reftableSymref     = 3
)

func (r *Repository) readReftableRef(name string, read func(string)) (refValue, error) {
	list := filepath.Join(r.GitDir, "reftable", "tables.list")
	data, err := os.ReadFile(list)
	if err != nil {
		return refValue{}, err
	}
	read(list)
	tables := strings.Fields(string(data))
	for _, table := range slices.Backward(tables) {
		table := filepath.Join(r.GitDir, "reftable", table)
		content, err := os.ReadFile(table)
		if err != nil {
			return refValue{}, err
		}
		read(table)
		value, present, err := reftableRef(content, name)
		if err != nil {
			return refValue{}, fmt.Errorf("%s: %w", table, err)
		}
		if present {
			return value, nil
		}
	}
	return refValue{}, nil
}

func reftableRef(data []byte, name string) (refValue, bool, error) {
	if len(data) < reftableHeaderV1 || string(data[:4]) != "REFT" {
		return refValue{}, false, errors.New("not a reftable")
	}
	header := reftableHeaderV1
	switch data[4] {
	case 1:
	case 2:
		header = reftableHeaderV2
	default:
		return refValue{}, false, fmt.Errorf("unsupported reftable version %d", data[4])
	}
	blockSize := uint24(data[5:])
	hashLength := 20
	if header == reftableHeaderV2 && string(data[reftableHeaderV1:reftableHeaderV2]) == "s256" {
		hashLength = 32
	}
	refEnd, err := reftableRefSectionEnd(data, header)
	if err != nil {
		return refValue{}, false, err
	}
	for offset, start := 0, header; start+4 <= refEnd; {
		if data[start] != 'r' {
			return refValue{}, false, nil
		}
		end := offset + uint24(data[start+1:])
		if end < start+4+2 || end > refEnd || blockSize > 0 && end > offset+blockSize {
			return refValue{}, false, errors.New("ref block length out of range")
		}
		value, present, passed, err := reftableBlockRef(data[start+4:end], hashLength, name)
		if err != nil || present || passed {
			return value, present, err
		}
		if blockSize > 0 {
			offset += blockSize
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

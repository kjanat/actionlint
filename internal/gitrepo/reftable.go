package gitrepo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	reftableHeaderV1 = 24
	reftableHeaderV2 = 28
	reftableSymref   = 3
)

func reftableBranch(gitDir string, read func(string)) (string, error) {
	list := filepath.Join(gitDir, "reftable", "tables.list")
	data, err := os.ReadFile(list)
	if err != nil {
		return "", err
	}
	read(list)
	tables := strings.Fields(string(data))
	for _, table := range slices.Backward(tables) {
		table := filepath.Join(gitDir, "reftable", table)
		content, err := os.ReadFile(table)
		if err != nil {
			return "", err
		}
		read(table)
		target, found, err := reftableHead(content)
		if err != nil {
			return "", fmt.Errorf("%s: %w", table, err)
		}
		if found {
			branch, ok := strings.CutPrefix(target, "refs/heads/")
			if !ok {
				return "", nil
			}
			return branch, nil
		}
	}
	return "", nil
}

func reftableHead(data []byte) (string, bool, error) {
	if len(data) < reftableHeaderV1 || string(data[:4]) != "REFT" {
		return "", false, errors.New("not a reftable")
	}
	header := reftableHeaderV1
	switch data[4] {
	case 1:
	case 2:
		header = reftableHeaderV2
	default:
		return "", false, fmt.Errorf("unsupported reftable version %d", data[4])
	}
	if len(data) < header+4 {
		return "", false, errors.New("truncated reftable")
	}
	if data[header] != 'r' {
		return "", false, nil
	}
	blockLen := int(binary.BigEndian.Uint32([]byte{0, data[header+1], data[header+2], data[header+3]}))
	if blockLen < header+4+2 || blockLen > len(data) {
		return "", false, errors.New("ref block length out of range")
	}
	restarts := int(binary.BigEndian.Uint16(data[blockLen-2:]))
	end := blockLen - 2 - 3*restarts
	if restarts == 0 || end < header+4 {
		return "", false, errors.New("ref block restart table out of range")
	}
	var name []byte
	for pos := header + 4; pos < end; {
		prefix, n, ok := reftableVarint(data[pos:end])
		if !ok {
			return "", false, errors.New("bad ref record")
		}
		pos += n
		packed, n, ok := reftableVarint(data[pos:end])
		if !ok {
			return "", false, errors.New("bad ref record")
		}
		pos += n
		suffix, kind := int(packed>>3), packed&7
		if prefix > uint64(len(name)) || pos+suffix > end {
			return "", false, errors.New("bad ref record")
		}
		name = append(name[:prefix], data[pos:pos+suffix]...)
		pos += suffix
		if _, n, ok = reftableVarint(data[pos:end]); !ok {
			return "", false, errors.New("bad ref record")
		}
		pos += n
		var value []byte
		switch kind {
		case 0:
		case 1:
			pos += hashLengthOf(data, header)
		case 2:
			pos += 2 * hashLengthOf(data, header)
		case reftableSymref:
			length, n, ok := reftableVarint(data[pos:end])
			if !ok || pos+n+int(length) > end {
				return "", false, errors.New("bad symref record")
			}
			value = data[pos+n : pos+n+int(length)]
			pos += n + int(length)
		default:
			return "", false, fmt.Errorf("unknown ref record type %d", kind)
		}
		if pos > end {
			return "", false, errors.New("bad ref record")
		}
		switch strings.Compare(string(name), "HEAD") {
		case 0:
			return string(value), true, nil
		case 1:
			return "", false, nil
		}
	}
	return "", false, nil
}

func hashLengthOf(data []byte, header int) int {
	if header == reftableHeaderV2 && string(data[reftableHeaderV1:reftableHeaderV2]) == "s256" {
		return 32
	}
	return 20
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

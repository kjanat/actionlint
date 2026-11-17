package testutil

import (
	"bytes"
	"errors"
)

// ReplaceWasmDataString replaces a unique equal-length fixture string in a
// WebAssembly module. Equal length keeps the fixture's embedded offsets valid.
func ReplaceWasmDataString(module, old, replacement []byte) ([]byte, error) {
	if len(old) == 0 || len(old) != len(replacement) {
		return nil, errors.New("WebAssembly fixture replacement must have equal nonzero lengths")
	}
	if len(module) < 8 || (!bytes.Equal(module[:8], []byte("\x00asm\x01\x00\x00\x00")) && !bytes.Equal(module[:8], []byte("\x00asm\x0d\x00\x01\x00"))) {
		return nil, errors.New("invalid WebAssembly module header")
	}
	if bytes.Count(module, old) != 1 {
		return nil, errors.New("WebAssembly fixture string must occur exactly once")
	}
	return bytes.Replace(module, old, replacement, 1), nil
}

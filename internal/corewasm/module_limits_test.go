package corewasm

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleSizeLimitIncludesDecompressedBytes(t *testing.T) {
	const limit = 1024
	for _, size := range []int{limit - 1, limit, limit + 1} {
		for _, compressed := range []bool{false, true} {
			data := bytes.Repeat([]byte{0}, size)
			if compressed {
				var encoded bytes.Buffer
				writer := gzip.NewWriter(&encoded)
				if _, err := writer.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				data = encoded.Bytes()
			}
			module, err := readModule(data, limit)
			if size <= limit {
				if err != nil || len(module) != size {
					t.Fatalf("size=%d compressed=%v: bytes=%d err=%v", size, compressed, len(module), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "size limit") || module != nil {
				t.Fatalf("oversized module accepted: compressed=%v bytes=%d err=%v", compressed, len(module), err)
			}
		}
	}
}

func TestModuleExpansionBombStopsAtBound(t *testing.T) {
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write(bytes.Repeat([]byte{0}, 1024*1024)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readModule(encoded.Bytes(), 4096); err == nil || !strings.Contains(err.Error(), "decompressed module exceeds") {
		t.Fatalf("expansion bomb was not rejected: %v", err)
	}
}

func TestLoadRejectsGzipExpansionBeforeCompilation(t *testing.T) {
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	block := make([]byte, 32*1024)
	for expanded := 0; expanded <= maxModuleBytes; expanded += len(block) {
		if _, err := writer.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "expansion.wasm.gz")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := Load(t.Context(), path, Options{})
	if engine != nil {
		_ = engine.Close(t.Context())
	}
	if err == nil || !strings.Contains(err.Error(), "decompressed module exceeds size limit") {
		t.Fatalf("oversized expansion reached compilation: %v", err)
	}
}

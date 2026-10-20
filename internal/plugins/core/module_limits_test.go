package core

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
)

func TestLoadRejectsGzipExpansionBeforeCompilation(t *testing.T) {
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write(bytes.Repeat([]byte{0}, 1024*1024)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "expansion.wasm.gz")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := Load(t.Context(), path, Options{Artifact: pluginartifact.Options{MaxBytes: 4096}})
	if engine != nil {
		_ = engine.Close(t.Context())
	}
	if err == nil || !strings.Contains(err.Error(), "plugin gzip") || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized expansion reached compilation: %v", err)
	}
}

func TestLoadArtifactRejectsInvalidMemoryLimit(t *testing.T) {
	artifact := &pluginartifact.Artifact{Info: pluginartifact.Info{Source: "in-memory"}}
	if _, err := LoadArtifact(t.Context(), artifact, Options{MemoryPages: 65537}); err == nil || !strings.Contains(err.Error(), "65536 pages") {
		t.Fatalf("in-memory artifact bypassed memory option validation: %v", err)
	}
}

func TestLoadArtifactRejectsUnsupportedMetadataBeforeCompilation(t *testing.T) {
	name := []byte(pluginartifact.MetadataSection)
	payload := append(binary.AppendUvarint(nil, uint64(len(name))), name...)
	payload = append(payload, []byte(`{"metadata_version":2,"abi_version":1}`)...)
	data := append([]byte("\x00asm\x01\x00\x00\x00"), 0)
	data = binary.AppendUvarint(data, uint64(len(payload)))
	data = append(data, payload...)
	artifact := &pluginartifact.Artifact{Info: pluginartifact.Info{Source: "in-memory"}, Bytes: data}
	if _, err := LoadArtifact(t.Context(), artifact, Options{}); err == nil || !strings.Contains(err.Error(), "custom metadata reports unsupported document version") {
		t.Fatalf("unsupported discovery metadata reached compilation: %v", err)
	}
}

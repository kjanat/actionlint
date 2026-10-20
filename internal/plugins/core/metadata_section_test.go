package core_test

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
	corewasm "actionlint.kjanat.dev/internal/plugins/core"
)

func TestCustomMetadataMustMatchRuntime(t *testing.T) {
	raw, err := os.ReadFile(enginePath("echo"))
	if err != nil {
		t.Fatal(err)
	}
	// Fixture engines predate custom sections; their runtime export remains a
	// supported discovery fallback. Serialize the full response from the fixture.
	engine := loadEngine(t, enginePath("echo"), corewasm.Options{})
	metadata := engine.Metadata()
	metadata.Engine.Name = "spoofed"
	declared, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	payload := binary.AppendUvarint(nil, uint64(len(pluginartifact.MetadataSection)))
	payload = append(payload, pluginartifact.MetadataSection...)
	payload = append(payload, declared...)
	section := binary.AppendUvarint([]byte{0}, uint64(len(payload)))
	section = append(section, payload...)
	path := filepath.Join(t.TempDir(), "spoofed.wasm")
	if err := os.WriteFile(path, append(raw, section...), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := corewasm.Load(t.Context(), path, corewasm.Options{})
	if loaded != nil {
		_ = loaded.Close(t.Context())
		t.Fatal("spoofed metadata loaded")
	}
	if err == nil || !strings.Contains(err.Error(), "does not match runtime metadata") {
		t.Fatal(err)
	}
}

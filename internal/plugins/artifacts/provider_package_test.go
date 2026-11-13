package artifacts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProcessProviderPackage(t *testing.T) {
	t.Parallel()
	const metadata = `{"metadata_version":1,"id":"example","name":"Example","description":"Example process provider","license":"MIT","repository":"https://github.com/publisher/plugin","plugin_version":"1.0.0","transport":"process"}`
	content := []byte(`{"schema_version":1,"transport":"process","metadata":` + metadata + `}`)
	path := filepath.Join(t.TempDir(), "provider.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(t.Context(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Info.Kind != ProviderManifest || resolved.Info.SHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) || !SameMetadata(resolved.Info.Metadata, []byte(metadata)) || string(resolved.Bytes) != string(content) {
		t.Fatalf("package framing or discovery identity changed: %+v", resolved.Info)
	}
	for _, data := range []string{
		`{"schema_version":1,"transport":"process","metadata":null}`,
		`{"schema_version":1,"transport":"process","metadata":[]}`,
		`{"schema_version":1,"transport":"process","metadata":{"id":"a","id":"b"}}`,
		`{"schema_version":1,"schema_version":1,"transport":"process"}`,
		`{"schema_version":1,"transport":"wasm-core"}`,
		`{"schema_version":2,"transport":"process"}`,
		strings.Repeat(" ", maxMetadataBytes) + `{}`,
	} {
		if _, _, err := SniffPackage([]byte(data)); err == nil {
			t.Fatalf("invalid package accepted: %.100s", data)
		}
	}
}

package packaging

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/v0.2.0/analyzer"
	"go.bytecodealliance.org/cm"
)

var componentHeader = []byte{0, 'a', 's', 'm', 13, 0, 1, 0}

func testSection(id byte, data []byte) []byte {
	section := binary.AppendUvarint([]byte{id}, uint64(len(data)))
	return append(section, data...)
}

func TestMetadataPreservesNestedComponents(t *testing.T) {
	inner, err := WithMetadata(componentHeader, []byte(`{"nested":true}`))
	if err != nil {
		t.Fatal(err)
	}
	nested := testSection(4, inner)
	input := append(bytes.Clone(componentHeader), nested...)
	first, err := WithMetadata(input, []byte(`{"old":true}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := WithMetadata(first, []byte(`{"new":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(second, nested) || bytes.Contains(second, []byte(`{"old":true}`)) || bytes.Count(second, []byte("actionlint.plugin")) != 2 {
		t.Fatal("replacing outer metadata changed nested bytes or duplicated discovery sections")
	}
	if !bytes.Contains(second, []byte(`{"new":true}`)) || !bytes.Contains(second, []byte(`{"nested":true}`)) {
		t.Fatal("outer or nested metadata is missing")
	}
}

func TestMetadataRejectsMalformedFraming(t *testing.T) {
	for _, suffix := range [][]byte{{1}, {1, 127}, {1, 128}, {0, 1, 127}, {1, 128, 128, 128, 128, 16}} {
		if _, err := WithMetadata(append(bytes.Clone(componentHeader), suffix...), []byte(`{}`)); err == nil {
			t.Fatalf("accepted malformed section %x", suffix)
		}
	}
	for _, metadata := range []string{"null", "[]", "invalid", strings.Repeat(" ", 1<<20) + "{}"} {
		if _, err := WithMetadata(componentHeader, []byte(metadata)); err == nil {
			t.Fatal("accepted non-object or oversized metadata")
		}
	}
	if _, err := WithMetadata([]byte{0, 'a', 's', 'm', 1, 0, 0, 0}, []byte(`{}`)); err == nil {
		t.Fatal("accepted a core module as a component")
	}
}

func TestPackageUsesExportedDescriptorAndWritesChecksums(t *testing.T) {
	plugin := Package{
		Identity: Identity{ID: "example/check", Name: "Example", Description: "Check scripts", License: "MIT", Repository: "https://github.com/example/check", Version: "1.0.0"},
		Engine:   Engine{Name: "check", Version: "1.0.0"}, Languages: []string{"bash"}, LicenseText: "license\r\n",
		Descriptor: analyzer.Descriptor{Name: "check", Description: "Check scripts", Category: "security", Maturity: "stable", OptionsSchema: `{"type":"object"}`, RequiredInputs: cm.ToList([]analyzer.InputKind{analyzer.InputKindScripts})},
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "raw.wasm")
	if err := os.WriteFile(input, componentHeader, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "package")
	if err := plugin.Write(input, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Interface  string `json:"interface"`
		WITSHA256  string `json:"wit_sha256"`
		Descriptor struct {
			Name           string   `json:"name"`
			RequiredInputs []string `json:"required_inputs"`
		} `json:"descriptor"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Interface != "kjanat:actionlint/analyzer@0.2.0" || metadata.WITSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(analyzerWIT, "\r\n", "\n")))) || metadata.Descriptor.Name != plugin.Descriptor.Name || len(metadata.Descriptor.RequiredInputs) != 1 || metadata.Descriptor.RequiredInputs[0] != "scripts" {
		t.Fatalf("metadata does not match the SDK contract and descriptor: %+v", metadata)
	}
	checksums, err := os.ReadFile(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.wasm", "metadata.json", "schema.json", "LICENSE"} {
		content, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(checksums), fmt.Sprintf("%x  %s\n", sha256.Sum256(content), name)) {
			t.Fatalf("checksum missing or incorrect for %s", name)
		}
	}
}

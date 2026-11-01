package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
	"actionlint.kjanat.dev/v2/rule"
)

func TestFacadeMetadataSnapshot(t *testing.T) {
	original := corewasm.Metadata{
		Rules:           []rule.Descriptor{{Name: "example", OptionsSchema: json.RawMessage(`{"type":"object"}`), Examples: []rule.Example{{Before: "before"}}}},
		MetadataVersion: 1, ABIVersion: 1, Transport: "wasm-core-json", ID: "example", Name: "Example", PluginVersion: "v1",
		Engine:       corewasm.EngineVersion{Name: "engine", Version: "v2"},
		Interfaces:   map[string][]corewasm.Interface{"check": {{Version: 3, Export: "check_v3"}}, "empty": nil},
		Capabilities: corewasm.Capabilities{ScriptShells: []string{"python"}, ScriptInputs: []analyzer.ScriptInput{{Action: "example/action", Input: "script", Language: "python"}}, Check: true, ValidateConfig: true, ConfigSchema: true, Fixes: true, ReportedSources: true, Resources: []analyzer.ToolResourceKind{analyzer.ToolDirectory}, Format: true, PositionEncoding: "utf-8", MaxInputBytes: 4096},
		ConfigSchema: json.RawMessage(`{"type":"object"}`), TemplateContract: "template-v1",
	}
	public := metadataFromInternal(original)
	roundtrip := public.internal()
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(roundtrip)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("metadata fields changed:\n%s\n%s", got, want)
	}
	public.Rules[0].OptionsSchema[0] = 'x'
	public.Rules[0].Examples[0].Before = "changed"
	public.Interfaces["check"][0].Export = "changed"
	public.Capabilities.ScriptShells[0] = "changed"
	public.Capabilities.ScriptInputs[0].Input = "changed"
	public.Capabilities.Resources[0] = analyzer.ToolWorkspace
	public.ConfigSchema[0] = 'x'
	for _, snapshot := range []corewasm.Metadata{original, roundtrip} {
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != string(want) {
			t.Fatal("public metadata retained mutable engine storage")
		}
	}
}

func TestFacadeOCIArtifactSnapshot(t *testing.T) {
	owned := &Artifact{Info: ArtifactInfo{Metadata: json.RawMessage(`{"id":"example"}`), Blobs: []string{"blob"}}, Bytes: []byte("module")}
	ref, err := pluginartifact.ParseOCI("oci://registry.example/example@sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	options := artifactOptionsToInternal(ArtifactOptions{PullOCI: func(context.Context, OCIReference, ArtifactOptions) (*Artifact, error) { return owned, nil }})
	acquired, err := options.PullOCI(context.Background(), ref, options)
	if err != nil {
		t.Fatal(err)
	}
	owned.Bytes[0] = 'x'
	owned.Info.Metadata[0] = 'x'
	owned.Info.Blobs[0] = "changed"
	if string(acquired.Bytes) != "module" || string(acquired.Info.Metadata) != `{"id":"example"}` || acquired.Info.Blobs[0] != "blob" {
		t.Fatal("OCI adapter retained mutable caller storage")
	}
}

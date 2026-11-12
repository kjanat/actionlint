package registry

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestRegistryReplacesPackagedArtifactMetadata(t *testing.T) {
	t.Parallel()
	publisher := PluginPublisher{PluginID: "original", BaseURL: "https://registry.example/", Name: "github.com/example/plugin", RepositoryID: 10}
	core := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	component := []byte{0, 'a', 's', 'm', 13, 0, 1, 0}
	child := writeArtifact(t, map[string][]byte{}, publisher, "v1.0.0", "plugin.wasm", core)
	// A core-module section is opaque to outer discovery metadata replacement.
	nested := append(bytes.Clone(component), 1)
	nested = binary.AppendUvarint(nested, uint64(len(child)))
	nested = append(nested, child...)
	for name, input := range map[string][]byte{"core": core, "component": component, "nested": nested} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			first := writeArtifact(t, map[string][]byte{}, publisher, "v1.0.0", "plugin.wasm", input)
			before := bytes.Clone(first)
			next := publisher
			next.PluginID = "replacement"
			second := writeArtifact(t, map[string][]byte{}, next, "v2.0.0", "plugin.wasm", first)
			_, metadata, err := artifacts.Sniff(second)
			if err != nil {
				t.Fatal(err)
			}
			var identity struct {
				ID            string `json:"id"`
				PluginVersion string `json:"plugin_version"`
			}
			if err := json.Unmarshal(metadata, &identity); err != nil {
				t.Fatal(err)
			}
			if identity.ID != "replacement" || identity.PluginVersion != "2.0.0" {
				t.Fatalf("replacement metadata: %s", metadata)
			}
			if !bytes.Equal(first, before) || name == "nested" && !bytes.Contains(second, child) {
				t.Fatal("replacement changed its input or nested module bytes")
			}
		})
	}
}

package installation

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestComponentWITChecksumFields(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	pin := Pin{Kind: artifacts.Component, Name: "github.com/example/plugin", PluginID: "example", Version: "v1.0.0", Contract: "example:plugin/checker@1.0.0#sha256=" + digest}
	for _, field := range []string{"wit_sha256", "contract_sha256"} {
		metadata := fmt.Sprintf(`{"metadata_version":1,"id":"example","plugin_version":"1.0.0","repository":"https://github.com/example/plugin","transport":"wasm-component","interface":"example:plugin/checker@1.0.0",%q:%q}`, field, digest)
		if err := pin.verifyMetadata([]byte(metadata)); err != nil {
			t.Errorf("%s: %v", field, err)
		}
	}
	conflicting := fmt.Sprintf(`{"metadata_version":1,"id":"example","plugin_version":"1.0.0","repository":"https://github.com/example/plugin","transport":"wasm-component","interface":"example:plugin/checker@1.0.0","wit_sha256":%q,"contract_sha256":%q}`, digest, strings.Repeat("b", 64))
	if err := pin.verifyMetadata([]byte(conflicting)); err == nil {
		t.Fatal("accepted conflicting WIT checksums")
	}
}

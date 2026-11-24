package pluginregistry

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestComponentContractIgnoresWITChecksum(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	metadata := `{"metadata_version":1,"id":"example","plugin_version":"1.0.0","repository":"https://github.com/example/plugin","transport":"wasm-component","interface":"example:plugin/checker@1.0.0"`
	for _, checksums := range []string{"", `,"wit_sha256":"` + strings.Repeat("b", 64) + `"`, `,"contract_sha256":"short"`, `,"wit_sha256":"` + digest + `","contract_sha256":"` + strings.Repeat("c", 64) + `"`} {
		for _, contract := range []string{"example:plugin/checker@1.0.0", "example:plugin/checker@1.0.0#sha256=" + digest} {
			pin := Pin{Kind: artifacts.Component, Name: "github.com/example/plugin", PluginID: "example", Version: "v1.0.0", Contract: contract}
			if err := pin.verifyMetadata([]byte(metadata + checksums + "}")); err != nil {
				t.Errorf("contract %q with %q: %v", contract, checksums, err)
			}
		}
	}
	pin := Pin{Kind: artifacts.Component, Name: "github.com/example/plugin", PluginID: "example", Version: "v1.0.0", Contract: "example:plugin/checker@2.0.0#sha256=" + digest}
	if err := pin.verifyMetadata([]byte(metadata + "}")); err == nil {
		t.Fatal("accepted a contract for another interface")
	}
}

func TestReleaseIdentityComparesContractInterface(t *testing.T) {
	t.Parallel()
	release := Pin{Registry: "https://catalogue.example/", Name: "github.com/example/plugin", Version: "v1.0.0", Selector: "v1.0.0", Contract: "example:plugin/checker@1.0.0#sha256=" + strings.Repeat("a", 64)}
	rehashed := release
	rehashed.Contract = "example:plugin/checker@1.0.0"
	other := release
	other.Contract = "example:plugin/checker@2.0.0#sha256=" + strings.Repeat("a", 64)
	if !sameRelease(release, rehashed) || sameRelease(release, other) {
		t.Fatal("release identity compares more or less than the contract interface")
	}
	release.Origin, rehashed.Origin, other.Origin = "candidate", "candidate", "candidate"
	if !sameCandidate(release, rehashed) || sameCandidate(release, other) {
		t.Fatal("candidate identity compares more or less than the contract interface")
	}
}

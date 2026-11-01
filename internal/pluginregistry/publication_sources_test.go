package pluginregistry

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRegistryV2PublicationSources(t *testing.T) {
	for _, source := range []string{"release-asset", "oci"} {
		t.Run(source, func(t *testing.T) {
			network := testutil.NewPluginRegistry(t)
			network.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
			url := network.RecordURL("v1.0.0")
			var published record
			if err := jsonv2.Unmarshal(network.File(url), &published); err != nil {
				t.Fatal(err)
			}
			submission := published.Submission
			submission.SchemaVersion = 2
			if source == "oci" {
				submission.Artifact = distribution{typeName: "oci", digest: submission.SHA256, reference: "oci://ghcr.io/publisher/plugin@sha256:" + submission.SHA256}
				published.URL = submission.Artifact.reference
			} else {
				submission.Artifact = distribution{typeName: "release-asset", name: submission.Asset, digest: submission.SHA256}
			}
			submission.Asset, submission.SHA256 = "", ""
			published.Submission, published.Receipt.Submission = submission, submission
			data, err := json.Marshal(published)
			if err != nil {
				t.Fatal(err)
			}
			network.SetFile(url, data)
			client, err := New(testutil.FixtureRegistryURL)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			if pin.SHA256 != submission.sourceDigest() || pin.Asset != submission.assetName() || source == "oci" && pin.Source() != submission.Artifact.reference {
				t.Fatalf("source graph identity changed: %+v", pin)
			}
			published.URL += "tampered"
			data, err = json.Marshal(published)
			if err != nil {
				t.Fatal(err)
			}
			network.SetFile(url, data)
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err == nil {
				t.Fatal("changed publication source accepted")
			}
		})
	}
}

func TestManifestPublicationIdentity(t *testing.T) {
	metadata := []byte(`{"metadata_version":1,"id":"example","plugin_version":"1.0.0","repository":"https://github.com/publisher/plugin","transport":"process"}`)
	pin := Pin{Registry: DefaultURL, Name: "github.com/publisher/plugin", RepositoryID: "7", OwnerID: "5", PluginID: "example", Version: "v1.0.0", Selector: "v1.0.0", Kind: artifact.ProviderManifest, Asset: "provider.json", URL: "https://github.com/publisher/plugin/releases/download/v1.0.0/provider.json", SHA256: strings.Repeat("a", 64), Contract: "process/1"}
	if err := pin.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := pin.Verify(artifact.Info{Kind: pin.Kind, SHA256: pin.SHA256, Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	pin.Contract = "wasm-core-json/1"
	if err := pin.Verify(artifact.Info{Kind: pin.Kind, SHA256: pin.SHA256, Metadata: metadata}); err == nil {
		t.Fatal("process manifest accepted with a Wasm contract")
	}
}

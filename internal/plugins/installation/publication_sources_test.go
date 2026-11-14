package installation

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestRegistryInspectionCannotGrantPrivateArtifactAccess(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	url := registry.RecordURL("v1.0.0")
	var published record
	if err := jsonv2.Unmarshal(registry.File(url), &published); err != nil {
		t.Fatal(err)
	}
	submission := published.Submission
	source := "oci://127.0.0.1:8443/private/plugin@sha256:" + submission.SHA256
	submission.SchemaVersion = 2
	submission.Artifact = distribution{typeName: "oci", digest: submission.SHA256, reference: source}
	submission.Asset, submission.SHA256 = "", ""
	published.Submission, published.Receipt.Submission, published.URL = submission, submission, source
	data, err := json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(url, data)
	ref, err := pluginref.New(fixtureregistry.FixturePluginName, "v1.0.0", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	acquisition := Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	if _, err := InspectRelease(t.Context(), ref, acquisition); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) {
		t.Fatalf("registry-selected private artifact error=%v, want private destination rejection", err)
	}
	for _, request := range registry.Requests() {
		if strings.Contains(request, "127.0.0.1") {
			t.Fatalf("registry caused a private network request: %s", request)
		}
	}
	dials := 0
	stopped := errors.New("test stopped the authorized connection")
	acquisition.Connections = acquisitionhttp.Connections{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, stopped
	}}}
	if _, err := Inspect(t.Context(), source, acquisition); !errors.Is(err, stopped) || dials == 0 {
		t.Fatalf("explicit artifact did not reach the selected endpoint: error=%v dials=%d", err, dials)
	}
}

func TestRegistryV2PublicationSources(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"release-asset", "oci"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			network := fixtureregistry.NewPluginRegistry(t)
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
			client, err := New(fixtureregistry.FixtureRegistryURL, Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()})
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
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
			if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err == nil {
				t.Fatal("changed publication source accepted")
			}
		})
	}
}

func TestManifestPublicationIdentity(t *testing.T) {
	t.Parallel()
	metadata := []byte(`{"metadata_version":1,"id":"example","plugin_version":"1.0.0","repository":"https://github.com/publisher/plugin","transport":"process"}`)
	pin := Pin{Registry: DefaultURL, Name: "github.com/publisher/plugin", RepositoryID: "7", OwnerID: "5", PluginID: "example", Version: "v1.0.0", Selector: "v1.0.0", Kind: artifacts.ProviderManifest, Asset: "provider.json", URL: "https://github.com/publisher/plugin/releases/download/v1.0.0/provider.json", SHA256: strings.Repeat("a", 64), Contract: "process/1"}
	if err := pin.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := pin.Verify(artifacts.Info{Kind: pin.Kind, SHA256: pin.SHA256, Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	pin.Contract = "wasm-core-json/1"
	if err := pin.Verify(artifacts.Info{Kind: pin.Kind, SHA256: pin.SHA256, Metadata: metadata}); err == nil {
		t.Fatal("process manifest accepted with a Wasm contract")
	}
}

func TestLegacyPublicationRejectsManifest(t *testing.T) {
	t.Parallel()
	data := `{"schema_version":1,"provider":"github","repository":"publisher/plugin","tag":"v1.0.0","asset":"provider.json","sha256":"` + strings.Repeat("a", 64) + `","kind":"manifest"}`
	var published submission
	if err := jsonv2.Unmarshal([]byte(data), &published); err == nil {
		t.Fatal("legacy publication accepted a process manifest; manifests require schema v2")
	}
}
